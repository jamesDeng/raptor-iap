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

type lateCancelFixture struct{ liveFixture }

func (f *lateCancelFixture) PutRequest(context.Context, string) error { return nil }
func (f *lateCancelFixture) SendSignal(context.Context, string, json.RawMessage) error {
	f.execution["status"] = "cancelled"
	return nil
}
func TestActionDelayedOutboxProjectsLateCancelledAnswer(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	raw, r := producerExecution(t)
	_, e := s.Pool.Exec(ctx, `INSERT INTO raptor.objects(id,kind,code,name) VALUES($1,'application',$2,'fixture')`, domain.NewID(), r.Definition.Object.Code)
	if e != nil {
		t.Fatal(e)
	}
	// Use the existing fixture environment and preserve the producer's result identities.
	r.Definition.EnvCode = "adev"
	created, e := s.Create(ctx, u, "late-cancel", r.Definition)
	if e != nil {
		t.Fatal(e)
	}
	raw["requestId"] = created.ID
	result := raw["result"].(map[string]any)
	result["requestId"] = created.ID
	for _, item := range result["evidence"].([]any) {
		identity := item.(map[string]any)["identity"].(map[string]any)
		identity["envCode"] = "adev"
		if identity["requestId"] != nil {
			identity["requestId"] = created.ID
		}
	}
	f := &lateCancelFixture{liveFixture{execution: raw}}
	s.Gateway = f
	if e = s.Action(ctx, created.ID, "cancel", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchPending(ctx, f); e != nil {
		t.Fatal(e)
	}
	v, e := s.View(ctx, created.ID)
	if e != nil || v.Request.Status != "cancelled" || v.CancellationPending || v.Execution["result"] == nil {
		t.Fatal("late cancellation lost answer or remained pending", e)
	}
}
