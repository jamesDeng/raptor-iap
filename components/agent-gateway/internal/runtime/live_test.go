package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/testutil"
	"net/http"
	"strings"
	"testing"
)

func TestTerminalRejectsIdentityPartialAndSecret(t *testing.T) {
	b := execution.AttemptBinding{RequestID: "request", AttemptID: "attempt"}
	for _, raw := range []string{`{"passed":true}`, `{"passed":false,"error":"Bearer private"}`, `{"passed":false,"error":"NeedsSignIn"} trailing`, `{"passed":false,"error":"NeedsSignIn"}`} {
		out, e := decodeTerminal([]byte(raw), b, []string{"private"})
		if raw == `{"passed":false,"error":"NeedsSignIn"}` {
			if e != nil || out.Error != "NeedsSignIn" {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal("invalid terminal accepted", raw)
		}
	}
}
func TestProgressOnlyConsumesCompleteContiguousRows(t *testing.T) {
	raw := `{"runtimeSequence":1,"kind":"status","outcome":"started","occurredAt":"2026-10-07T00:00:00Z"}` + "\n" + `{"runtimeSequence":2`
	events, e := decodeProgress([]byte(raw), 0)
	if e != nil || len(events) != 1 {
		t.Fatal(events, e)
	}
	if _, e = decodeProgress([]byte(strings.Replace(raw, "runtimeSequence\":1", "runtimeSequence\":3", 1)), 0); e == nil {
		t.Fatal("gap accepted")
	}
}

type nativeLease struct{}

func (nativeLease) Valid(context.Context) bool { return true }
func nativeJournal(t *testing.T) (*NativeLive, execution.RecoveryRecord) {
	p := testutil.Database(t)
	if e := db.Migrate(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	s := &execution.Store{Pool: p}
	id := execution.NewID()
	s.Receive(context.Background(), id)
	x, e := s.ClaimNext(context.Background(), "owner")
	if e != nil {
		t.Fatal(e)
	}
	b := execution.AttemptBinding{RequestID: id, AttemptID: x.AttemptID, Operation: "application.question", ObjectKind: "application", ObjectCode: "app", EnvCode: "rdev.ali", SkillsCommit: strings.Repeat("a", 40), Model: "gpt-5.6-luna"}
	if e = s.BindLive(context.Background(), b.AttemptID, "owner", b, strings.Repeat("b", 64)); e != nil {
		t.Fatal(e)
	}
	n := &NativeLive{Store: s, Owner: "owner", Lease: nativeLease{}, Management: Management{API: &keyAPI{removed: true}, TeamID: "team", AccountID: "account"}}
	r, e := s.RecoveryRecord(context.Background(), b.AttemptID)
	if e != nil {
		t.Fatal(e)
	}
	return n, r
}
func TestCleanupInventoryCannotHideKnownSandbox(t *testing.T) {
	n, r := nativeJournal(t)
	ctx := context.Background()
	n.intent(ctx, r.Binding, "sandbox", r.Binding.AttemptID)
	n.resource(ctx, r.Binding, "sandbox", "known")
	r, _ = n.Store.RecoveryRecord(ctx, r.Binding.AttemptID)
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v2/sandboxes":
			w.Write([]byte(`[]`))
		case "/sandboxes/known":
			w.Write([]byte(`{"sandboxID":"known"}`))
		default:
			t.Fatal(req.URL.Path)
		}
	})
	defer close()
	out, e := n.cleanup(ctx, r, tr)
	if e == nil || out.SandboxAbsent {
		t.Fatal("inventory omission falsely confirmed absence")
	}
}
func TestCleanupNoRuntimeResourcesRequiresNoCloudCall(t *testing.T) {
	n, r := nativeJournal(t)
	out, e := n.cleanup(context.Background(), r, nil)
	if e != nil || !out.SandboxAbsent || !out.KeyAbsent {
		t.Fatal(out, e)
	}
}
func TestUnknownCreateRemainsBlockedAfterEmptyInventory(t *testing.T) {
	n, r := nativeJournal(t)
	ctx := context.Background()
	n.intent(ctx, r.Binding, "sandbox", r.Binding.AttemptID)
	r, _ = n.Store.RecoveryRecord(ctx, r.Binding.AttemptID)
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, req *http.Request) { w.Write([]byte(`[]`)) })
	defer close()
	out, e := n.cleanup(ctx, r, tr)
	if e == nil || out.SandboxAbsent {
		t.Fatal("unresolved create released slot")
	}
}

func TestMixedMCPAuthentication(t *testing.T) {
	var c LiveConfig
	json.Unmarshal([]byte(`{"raptorMcpUrl":"https://raptor.fixture/mcp","infraMcpUrl":"https://infra.fixture/mcp","infraUsername":"reader","infraPassword":"fixture-only"}`), &c)
	raw, e := liveJob(c, execution.LiveStart{Access: execution.AgentAccess{Credential: "opaque-fixture"}, Binding: execution.AttemptBinding{RequestID: "request"}, Question: "healthy?"})
	if e != nil {
		t.Fatal(e)
	}
	var job struct {
		Request json.RawMessage
		MCP     map[string]struct{ Headers map[string]string }
	}
	json.Unmarshal(raw, &job)
	if job.MCP["raptor"].Headers["Authorization"] != "Bearer opaque-fixture" || job.MCP["infra"].Headers["X-Infra-Authorization"] != "Basic "+base64.StdEncoding.EncodeToString([]byte("reader:fixture-only")) {
		t.Fatal("MCP authentication not separated")
	}
	if strings.Contains(string(job.Request), "fixture-only") || strings.Contains(string(job.Request), "opaque-fixture") {
		t.Fatal("credential exposed as model input")
	}
}
