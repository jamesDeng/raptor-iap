package backend

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"testing"
	"time"
)

type resumeDiagnosticGateway struct{ view map[string]any }

func (g resumeDiagnosticGateway) Execution(context.Context, string) (map[string]any, error) {
	return g.view, nil
}
func (g resumeDiagnosticGateway) Progress(context.Context, string, int64) ([]requests.GatewayEvent, error) {
	return nil, nil
}
func TestResourceResumeAccessDoesNotDependOnBrowserPoll(t *testing.T) {
	s, r, hash := accessFixture(t)
	ctx := context.Background()
	attempt := domain.NewID()
	if _, e := s.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status='waiting_resource' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	s.Requests.Gateway = resumeDiagnosticGateway{map[string]any{"requestId": r.ID, "attemptId": attempt, "status": "running", "runtimeMode": "live", "stage": "preparing", "updatedAt": time.Now().UTC().Format(time.RFC3339Nano)}}
	input := map[string]any{"attemptId": attempt, "definitionSha256": hash, "expiresAt": time.Now().Add(time.Minute)}
	before := accessCall(s, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", input)
	if before.Code != 201 {
		t.Fatalf("resource resume requires access without browser poll, got %d", before.Code)
	}
	if _, e := s.Requests.View(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	after := accessCall(s, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", input)
	if after.Code != 201 {
		t.Fatalf("same attempt after browser poll should succeed, got %d", after.Code)
	}

}

type handoffGateway struct {
	view    map[string]any
	before  func()
	offline bool
}

func (g handoffGateway) Execution(context.Context, string) (map[string]any, error) {
	if g.before != nil {
		g.before()
	}
	if g.offline {
		return nil, domain.ErrUnavailable
	}
	return g.view, nil
}
func (g handoffGateway) Progress(context.Context, string, int64) ([]requests.GatewayEvent, error) {
	return nil, nil
}
func TestResourceResumeHandoffFailsClosed(t *testing.T) {
	for _, scenario := range []string{"wrong-attempt", "wrong-request", "waiting", "running-stage", "offline", "stale", "cancel-race", "block-race", "failed-race", "wrong-definition"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, hash := accessFixture(t)
			ctx := context.Background()
			attempt := domain.NewID()
			if _, e := s.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status='waiting_resource' WHERE id=$1", r.ID); e != nil {
				t.Fatal(e)
			}
			view := map[string]any{"requestId": r.ID, "attemptId": attempt, "status": "running", "runtimeMode": "live", "stage": "preparing", "updatedAt": time.Now().UTC().Format(time.RFC3339Nano)}
			g := handoffGateway{view: view}
			switch scenario {
			case "wrong-attempt":
				view["attemptId"] = domain.NewID()
			case "wrong-request":
				view["requestId"] = domain.NewID()
			case "waiting":
				view["status"] = "waiting_resource"
			case "running-stage":
				view["stage"] = "running"
			case "offline":
				g.offline = true
			case "wrong-definition":
				hash = "wrong"
			case "stale":
				old := map[string]any{"requestId": r.ID, "attemptId": domain.NewID(), "status": "waiting_resource", "runtimeMode": "live", "updatedAt": time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
				raw, _ := json.Marshal(old)
				if _, e := s.Requests.Pool.Exec(ctx, "INSERT INTO raptor.execution_views(request_id,public_view,synced_at) VALUES($1,$2,now())", r.ID, raw); e != nil {
					t.Fatal(e)
				}
			case "cancel-race", "block-race", "failed-race":
				state := map[string]string{"cancel-race": "cancelled", "block-race": "blocked", "failed-race": "failed"}[scenario]
				control := state
				if state == "failed" {
					control = ""
				}
				g.before = func() {
					if _, e := s.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status=$2,control_state=$3 WHERE id=$1", r.ID, state, control); e != nil {
						t.Fatal(e)
					}
				}
			}
			s.Requests.Gateway = g
			out := accessCall(s, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", map[string]any{"attemptId": attempt, "definitionSha256": hash, "expiresAt": time.Now().Add(time.Minute)})
			if out.Code == 201 {
				t.Fatal("unsafe handoff granted access", scenario)
			}
			var count int
			if e := s.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1", r.ID).Scan(&count); e != nil || count != 0 {
				t.Fatal("token created", count, e)
			}
		})
	}
}
