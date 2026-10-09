package bridge

import (
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/raptor"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/state"
	"strings"
	"unicode/utf8"
)

func requestMatches(q raptor.Request, s state.Session) bool {
	var d raptor.Definition
	if json.Unmarshal(s.Payload, &d) != nil || len(d.Operations) != 1 {
		return false
	}
	v := q.Definition
	return q.ID != "" && (s.RequestID == "" || q.ID == s.RequestID) && q.CreatorID == s.Binding.UserID && v.Type == "agent" && v.Model == s.Binding.Model && v.Object.Kind == "application" && v.Object.Code == s.Binding.App && v.EnvCode == s.Binding.Env && len(v.Operations) == 1 && v.Operations[0].Name == "application.question" && len(v.Operations[0].Parameters) == 1 && v.Operations[0].Parameters["question"] == d.Operations[0].Parameters["question"]
}
func validResult(s state.Session, v raptor.RequestView) error {
	x := v.Execution
	if x == nil || !v.ExecutionAvailable || v.CancellationPending || x.RuntimeMode != "live" || x.RequestID != s.RequestID || x.AttemptID == "" || x.AttemptID != s.AttemptID || x.RecoveryNeeded || x.CheckpointStatus != "verified" || !x.CleanupStatus.Confirmed() {
		return ErrBinding
	}
	a := x.Result
	if a == nil || a.RequestID != s.RequestID || a.AttemptID != x.AttemptID || a.SelectedModel != s.Binding.Model || a.ActualModel != s.Binding.Model || strings.TrimSpace(a.Answer) == "" || !utf8.ValidString(a.Answer) || len(a.Answer) > 16384 || a.GeneratedAt.IsZero() || len(a.Usage) == 0 || len(a.Evidence) > 32 {
		return ErrBinding
	}
	var total int64
	for _, u := range a.Usage {
		if u.Input < 0 || u.Output < 0 || u.Total < 0 || u.Total > 9007199254740991-total {
			return ErrBinding
		}
		total += u.Total
	}
	if total == 0 {
		return ErrBinding
	}
	seen := map[string]bool{}
	for _, ev := range a.Evidence {
		if ev.ObservedAt.IsZero() || ev.Identity.EnvCode != s.Binding.Env || len(ev.State) > 4096 {
			return ErrBinding
		}
		id := ev.Identity
		if id.RequestID != "" && id.RequestID != s.RequestID {
			return ErrBinding
		}
		if id.ObjectCode != "" && id.ObjectCode != s.Binding.App || id.AppCode != "" && id.AppCode != s.Binding.App {
			return ErrBinding
		}
		if ev.Server == "raptor" && ev.EvidenceMode == "catalog" && ev.Tool == "request_get" && id.RequestID == s.RequestID && id.ObjectCode == s.Binding.App {
			seen["context"] = true
		}
		if ev.Server == "infra" && ev.EvidenceMode == "live" {
			switch ev.Tool {
			case "cloud_identity_get":
				var st struct {
					Match bool `json:"accountMatches"`
				}
				if json.Unmarshal(ev.State, &st) != nil || !st.Match {
					return ErrBinding
				}
				seen["identity"] = true
			case "deployments_list":
				seen["list"] = true
			case "deployment_status_get":
				seen["status"] = true
			}
		}
	}
	for _, k := range []string{"context", "identity", "list", "status"} {
		if !seen[k] {
			return ErrBinding
		}
	}
	return nil
}
