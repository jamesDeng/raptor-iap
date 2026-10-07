package requests

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"os"
	"testing"
)

func producerExecution(t *testing.T) (map[string]any, domain.Request) {
	t.Helper()
	read := func(name string) []byte {
		b, e := os.ReadFile("../../../../scripts/contracts/live-question/fixtures/" + name)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	var raw map[string]any
	json.Unmarshal(read("gateway-execution.json"), &raw)
	var ctx struct {
		RequestID  string
		Definition domain.RequestInput
	}
	json.Unmarshal(read("raptor-context.json"), &ctx)
	return raw, domain.Request{ID: ctx.RequestID, Definition: ctx.Definition}
}
func TestGatewayResultProjectsIntoRaptor(t *testing.T) {
	raw, r := producerExecution(t)
	out, e := publicExecution(raw, r)
	if e != nil {
		t.Fatal("real producer result rejected", e)
	}
	result := out["result"].(map[string]any)
	usage := result["usage"].([]any)
	if usage[0].(map[string]any)["input"] != float64(100) || result["answer"] != "Application is ready at this observation." {
		t.Fatal("producer information lost")
	}
}
func TestProjectionRejectsForeignOrIncompleteResult(t *testing.T) {
	for _, which := range []string{"request", "attempt", "model", "env", "uid", "usage", "count", "cleanup", "account"} {
		t.Run(which, func(t *testing.T) {
			raw, r := producerExecution(t)
			result := raw["result"].(map[string]any)
			evidence := result["evidence"].([]any)
			switch which {
			case "request":
				result["requestId"] = "other"
			case "attempt":
				result["attemptId"] = "other"
			case "model":
				result["actualModel"] = "other"
			case "env":
				evidence[3].(map[string]any)["identity"].(map[string]any)["envCode"] = "other"
			case "uid":
				evidence[3].(map[string]any)["identity"].(map[string]any)["uid"] = "other"
			case "usage":
				result["usage"].([]any)[0].(map[string]any)["input"] = -1
			case "count":
				for len(evidence) < 33 {
					evidence = append(evidence, evidence[0])
				}
				result["evidence"] = evidence
			case "cleanup":
				raw["cleanupStatus"].(map[string]any)["sandboxAbsent"] = false
			case "account":
				evidence[1].(map[string]any)["state"] = map[string]any{"accountMatches": false}
			}
			if _, e := publicExecution(raw, r); e == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestCancellationPendingRejectsConcurrentCompletion(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "race", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE raptor.requests SET control_state='cancel_requested' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	s.Gateway = &liveFixture{execution: map[string]any{"requestId": r.ID, "attemptId": "a", "runtimeMode": "live", "status": "completed"}}
	v, e := s.View(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if v.Request.Status == "completed" || !v.CancellationPending {
		t.Fatal("concurrent completion defeated cancellation")
	}
}
