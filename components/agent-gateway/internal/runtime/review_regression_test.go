package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReviewTerminalPollKeepsConversationTurn(t *testing.T) {
	a, f, in := axFixture(t)
	b := in.Binding
	now := time.Now()
	id := execution.EvidenceIdentity{RequestID: b.RequestID, EnvCode: b.EnvCode, ObjectCode: b.ObjectCode, ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "uid"}
	result := execution.LiveResult{RequestID: b.RequestID, AttemptID: b.AttemptID, SelectedModel: b.Model, ActualModel: b.Model, Answer: "verified answer", GeneratedAt: now}
	for _, tool := range []string{"request_get", "cloud_identity_get", "deployments_list", "deployment_status_get"} {
		server, mode := "infra", "live"
		if tool == "request_get" {
			server, mode = "raptor", "catalog"
		}
		result.Evidence = append(result.Evidence, execution.ToolEvidence{Server: server, Tool: tool, ObservedAt: now, EvidenceMode: mode, Identity: id, State: json.RawMessage(`{"accountMatches":true}`)})
	}
	if e := execution.ValidateLiveResult(result, b); e != nil {
		t.Fatal(e)
	}
	turnID := execution.NewID()
	turn := execution.ConversationTurn{TurnID: turnID, Result: result}
	f.files["/tmp/raptor-private/conversation-turn-"+turnID+".json"], _ = json.Marshal(turn)
	event := execution.RuntimeEvent{RuntimeSequence: 1, Kind: "turn", TurnID: turnID, OccurredAt: now}
	raw, _ := json.Marshal(event)
	f.files["/tmp/raptor-private/attempt-progress.jsonl"] = append(raw, '\n')
	f.files["/tmp/raptor-private/attempt-result.json"], _ = json.Marshal(map[string]any{"phase": "live-inference", "passed": true, "result": result})
	a.axRuns = map[string]*axRun{b.RequestID: {start: in}}
	f.inferenceTerminal = true
	out, e := a.Poll(context.Background(), execution.RuntimeHandle{RequestID: b.RequestID, ID: "raptor-" + b.AttemptID}, 0)
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Turns) != 1 {
		t.Fatalf("terminal poll kept %d events but lost conversation payload: turns=%d", len(out.Events), len(out.Turns))
	}
}

func TestLiveJobNeverGivesSandboxServiceInfraSecret(t *testing.T) {
	a, _, in := axFixture(t)
	raw, err := liveJob(a.Config, in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), base64.StdEncoding.EncodeToString([]byte(a.Config.InfraUsername+":"+a.Config.InfraPassword))) {
		t.Fatal("shared Infra service secret delivered to sandbox")
	}
	var job struct {
		MCP map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcp"`
	}
	if json.Unmarshal(raw, &job) != nil {
		t.Fatal("invalid job")
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("agent:"+in.Access.Credential))
	if job.MCP["infra"].Headers["X-Infra-Authorization"] != want {
		t.Fatal("agent attempt credential missing")
	}
}

func TestNativeTerminalPollKeepsConversationTurn(t *testing.T) {
	a, f, in := axFixture(t)
	b := in.Binding
	now := time.Now()
	turnID := execution.NewID()
	result := execution.LiveResult{RequestID: b.RequestID, AttemptID: b.AttemptID, SelectedModel: b.Model, ActualModel: b.Model, Answer: "verified answer", GeneratedAt: now}
	id := execution.EvidenceIdentity{RequestID: b.RequestID, EnvCode: b.EnvCode, ObjectCode: b.ObjectCode, ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "uid"}
	for _, tool := range []string{"request_get", "cloud_identity_get", "deployments_list", "deployment_status_get"} {
		server, mode := "infra", "live"
		if tool == "request_get" {
			server, mode = "raptor", "catalog"
		}
		result.Evidence = append(result.Evidence, execution.ToolEvidence{Server: server, Tool: tool, ObservedAt: now, EvidenceMode: mode, Identity: id, State: json.RawMessage(`{"accountMatches":true}`)})
	}
	f.files["/tmp/raptor-private/conversation-turn-"+turnID+".json"], _ = json.Marshal(execution.ConversationTurn{TurnID: turnID, Result: result})
	row, _ := json.Marshal(execution.RuntimeEvent{RuntimeSequence: 1, Kind: "turn", TurnID: turnID, OccurredAt: now})
	f.files["/tmp/raptor-private/attempt-progress.jsonl"] = append(row, '\n')
	f.files["/tmp/raptor-private/attempt-result.json"], _ = json.Marshal(map[string]any{"phase": "live-inference", "passed": true, "result": result})
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
		raw, ok := f.files[r.URL.Query().Get("path")]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Write(raw)
	})
	defer close()
	done := make(chan error, 1)
	done <- nil
	a.runs = map[string]*nativeRun{b.RequestID: {start: in, transport: tr, sandbox: SandboxRef{ID: "sandbox", EnvdVersion: "0.5.7", accessToken: "fixture-envd"}, done: done}}
	out, err := a.NativeLive.Poll(context.Background(), execution.RuntimeHandle{RequestID: b.RequestID, ID: "sandbox"}, 0)
	if err != nil || len(out.Turns) != 1 {
		t.Fatal("terminal conversation lost", len(out.Turns), err)
	}
}
