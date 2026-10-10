package execution

import "time"

const ReplacementConvergenceAnswer = "Replacement converged; retained traffic and approval assessment is pending."

type ReplacementScaleAction struct {
	ActionID string `json:"actionId"`
	Previous int    `json:"previousDesiredCapacity"`
	Desired  int    `json:"desiredCapacity"`
	Outcome  string `json:"outcome"`
}
type ReplacementConvergence struct {
	Version         int                      `json:"version"`
	Converged       bool                     `json:"converged"`
	Acceptance      string                   `json:"acceptance"`
	Scope           ReplacementScope         `json:"scope"`
	OldInstanceIDs  []string                 `json:"oldInstanceIds"`
	NewInstanceIDs  []string                 `json:"newInstanceIds"`
	DesiredCapacity int                      `json:"desiredCapacity"`
	Actions         []ReplacementScaleAction `json:"actions"`
	ObservedAt      time.Time                `json:"observedAt"`
}

func validateReplacementResult(r LiveResult, b AttemptBinding) error {
	v := r.Replacement
	if !b.journalValid() || v == nil || v.Version != 1 || !v.Converged || v.Acceptance != "pending" || !v.Scope.ValidFor(b) || v.DesiredCapacity != 2 || v.ObservedAt.IsZero() || r.Answer != ReplacementConvergenceAnswer || len(r.Evidence) != 0 || len(v.OldInstanceIDs) != 2 || len(v.NewInstanceIDs) != 2 || len(v.Actions) != 3 {
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
	actions := map[string]bool{}
	for i, a := range v.Actions {
		if !journalName.MatchString(a.ActionID) || actions[a.ActionID] || a.Previous != stages[i][0] || a.Desired != stages[i][1] || (a.Outcome != "submitted" && a.Outcome != "unknown") {
			return ErrInvalid
		}
		actions[a.ActionID] = true
	}
	return validateUsage(r.Usage)
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
