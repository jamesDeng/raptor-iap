package execution

import (
	"slices"
	"time"
)

const PgcatAcceptanceAnswer = "PgCat replacement and SQL traffic acceptance passed."

const ReplacementConvergenceAnswer = "Replacement converged; retained traffic and approval assessment is pending."

type ReplacementScaleAction struct {
	ActionID          string `json:"actionId"`
	Previous          int    `json:"previousDesiredCapacity"`
	Desired           int    `json:"desiredCapacity"`
	Outcome           string `json:"outcome"`
	ScalingActivityID string `json:"scalingActivityId,omitempty"`
}
type ReplacementConvergence struct {
	RestartGeneration int64                    `json:"restartGeneration,omitempty"`
	Version           int                      `json:"version"`
	Converged         bool                     `json:"converged"`
	Acceptance        string                   `json:"acceptance"`
	Scope             ReplacementScope         `json:"scope"`
	OldInstanceIDs    []string                 `json:"oldInstanceIds"`
	NewInstanceIDs    []string                 `json:"newInstanceIds"`
	DesiredCapacity   int                      `json:"desiredCapacity"`
	Actions           []ReplacementScaleAction `json:"actions"`
	ObservedAt        time.Time                `json:"observedAt"`
	Traffic           *PgcatResultTraffic      `json:"traffic,omitempty"`
	FinalTraffic      *PgcatResultTraffic      `json:"finalTraffic,omitempty"`
	Retirement        *PgcatResultRetirement   `json:"retirement,omitempty"`
}
type PgcatResultTraffic struct {
	RestartActionID string    `json:"restartActionId"`
	ApplicationUID  string    `json:"applicationUid"`
	Generation      int64     `json:"generation"`
	ObservedAt      time.Time `json:"observedAt"`
	NoErrors        bool      `json:"noErrors"`
	Nodes           map[string]struct {
		Queries      int64 `json:"queries"`
		Transactions int64 `json:"transactions"`
	} `json:"nodes"`
}
type PgcatResultRetirement struct {
	ActionID          string    `json:"actionId"`
	ScalingActivityID string    `json:"scalingActivityId"`
	Status            string    `json:"status"`
	DestroyedIDs      []string  `json:"destroyedIds"`
	ObservedAt        time.Time `json:"observedAt"`
}

func validateReplacementResult(r LiveResult, b AttemptBinding) error {
	v := r.Replacement
	if !b.journalValid() || v == nil || (v.Version != 1 && v.Version != 2 && v.Version != 3) || !v.Converged || !v.Scope.ValidFor(b) || v.DesiredCapacity != 2 || v.ObservedAt.IsZero() || len(r.Evidence) != 0 || len(v.OldInstanceIDs) != 2 || len(v.NewInstanceIDs) != 2 {
		return ErrInvalid
	}
	if b.ApprovalProtocol == "request-grants-v1" {
		if v.Version != 3 || v.Acceptance != "verified" || r.Answer != PgcatAcceptanceAnswer || !validPgcatTrafficResult(v) {
			return ErrInvalid
		}
	} else if v.Version == 3 || v.Acceptance != "pending" || r.Answer != ReplacementConvergenceAnswer ||
		v.Traffic != nil || v.FinalTraffic != nil || v.Retirement != nil {
		return ErrInvalid
	}
	ids := map[string]bool{}
	for _, id := range append(append([]string{}, v.OldInstanceIDs...), v.NewInstanceIDs...) {
		if !journalName.MatchString(id) || ids[id] {
			return ErrInvalid
		}
		ids[id] = true
	}
	stages := [][2]int{{2, 4}, {4, 3}, {3, 2}}
	if v.Version >= 2 {
		stages = [][2]int{{2, 4}, {4, 2}}
		if v.RestartGeneration < 1 || v.RestartGeneration > 9007199254740991 {
			return ErrInvalid
		}
	} else if v.RestartGeneration != 0 {
		return ErrInvalid
	}
	if len(v.Actions) != len(stages) {
		return ErrInvalid
	}
	actions := map[string]bool{}
	for i, a := range v.Actions {
		if !journalName.MatchString(a.ActionID) || actions[a.ActionID] || a.Previous != stages[i][0] || a.Desired != stages[i][1] || (a.Outcome != "submitted" && a.Outcome != "unknown") {
			return ErrInvalid
		}
		if v.Version == 3 && a.Outcome != "submitted" {
			return ErrInvalid
		}
		actions[a.ActionID] = true
	}
	return validateUsage(r.Usage)
}
func validPgcatTrafficResult(v *ReplacementConvergence) bool {
	old, retirement := v.OldInstanceIDs, v.Retirement
	if retirement == nil || retirement.Status != "Successful" || !journalName.MatchString(retirement.ScalingActivityID) ||
		len(v.Actions) != 2 || retirement.ActionID != v.Actions[1].ActionID || retirement.ScalingActivityID != v.Actions[1].ScalingActivityID ||
		len(retirement.DestroyedIDs) != 2 || retirement.DestroyedIDs[0] == retirement.DestroyedIDs[1] ||
		!slices.Contains(old, retirement.DestroyedIDs[0]) || !slices.Contains(old, retirement.DestroyedIDs[1]) ||
		retirement.ObservedAt.IsZero() || retirement.ObservedAt.After(v.ObservedAt) {
		return false
	}
	for _, proof := range []*PgcatResultTraffic{v.Traffic, v.FinalTraffic} {
		if proof == nil || !proof.NoErrors || !journalName.MatchString(proof.RestartActionID) || proof.ApplicationUID != v.Scope.Application.UID ||
			proof.Generation != v.RestartGeneration || proof.ObservedAt.IsZero() || proof.ObservedAt.After(v.ObservedAt) || len(proof.Nodes) != 2 {
			return false
		}
		for _, id := range v.NewInstanceIDs {
			counter := proof.Nodes[id]
			if counter.Queries < 1 || counter.Transactions < 1 {
				return false
			}
		}
	}
	return v.Traffic.RestartActionID == v.FinalTraffic.RestartActionID && v.FinalTraffic.ObservedAt.After(v.Traffic.ObservedAt)
}

func validateUsage(rows []Usage) error {
	var input, output, total int64
	const max int64 = 9007199254740991
	for _, u := range rows {
		if u.Input < 0 || u.Output < 0 || u.TotalTokens < 0 || u.Input > max-input || u.Output > max-output || u.TotalTokens > max-total {
			return ErrInvalid
		}
		input += u.Input
		output += u.Output
		total += u.TotalTokens
	}
	return nil
}
