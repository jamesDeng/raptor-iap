package runtime

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestConversationRuntimeDeliveryNativeAX(t *testing.T) {
	n, r := nativeJournal(t)
	var nativeBytes []byte
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("path") != "/tmp/raptor-private/conversation-inbox.json" {
			t.Error("wrong inbox")
		}
		if e := req.ParseMultipartForm(32768); e != nil {
			t.Error(e)
		}
		f, _, e := req.FormFile("file")
		if e != nil {
			t.Error(e)
			return
		}
		defer f.Close()
		nativeBytes, _ = io.ReadAll(f)
		w.Write([]byte(`[]`))
	})
	defer close()
	n.runs = map[string]*nativeRun{r.Binding.RequestID: {start: execution.LiveStart{Binding: r.Binding}, transport: tr, sandbox: SandboxRef{ID: "sandbox"}}}
	in := execution.ConversationInput{RequestID: r.Binding.RequestID, MessageID: execution.NewID(), ActorID: execution.NewID(), InputSequence: 1, Text: "/skill:literal", AcceptedAt: time.Now()}
	if e := n.DeliverMessage(context.Background(), execution.RuntimeHandle{RequestID: in.RequestID, ID: "sandbox"}, in); e != nil {
		t.Fatal(e)
	}
	var wire map[string]any
	json.Unmarshal(nativeBytes, &wire)
	if wire["text"] != in.Text || wire["attemptId"] != r.Binding.AttemptID {
		t.Fatal("native identity/text")
	}
	a, f, start := axFixture(t)
	a.axRuns = map[string]*axRun{start.Binding.RequestID: {start: start}}
	in.RequestID = start.Binding.RequestID
	if e := a.DeliverMessage(context.Background(), execution.RuntimeHandle{RequestID: in.RequestID, ID: "raptor-" + start.Binding.AttemptID}, in); e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(f.files["/tmp/raptor-private/conversation-inbox.json"], &wire)
	if wire["text"] != in.Text || wire["attemptId"] != start.Binding.AttemptID {
		t.Fatal("AX identity/text")
	}
	a.Owner = "stale"
	if e := a.DeliverMessage(context.Background(), execution.RuntimeHandle{RequestID: in.RequestID, ID: "raptor-" + start.Binding.AttemptID}, in); e == nil {
		t.Fatal("stale AX owner wrote")
	}
}

func TestApplicationEvidenceAppCodeAcrossAXResultDecoders(t *testing.T) {
	b := execution.AttemptBinding{RequestID: execution.NewID(), AttemptID: execution.NewID(), Operation: "application.question",
		ObjectKind: "application", ObjectCode: "A00003", EnvCode: "rdev.ali", ClusterID: "cluster",
		Model: "gpt-6.1-sol", ProviderID: "codex", ConnectionVersion: 1,
		SkillsCommit: "645245d297db872765369b76728a00cb4cb53c8b"}
	turnID := execution.NewID()
	events := []execution.RuntimeEvent{{Kind: "turn", TurnID: turnID}}
	for _, tc := range []struct {
		name, fields string
		want         bool
	}{
		{name: "AX guest application code", fields: `,"appCode":"A00003"`, want: true},
		{name: "older guest", want: true},
		{name: "different application", fields: `,"appCode":"A00004"`},
		{name: "unrecognized identity field", fields: `,"foreign":"A00003"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := `{"requestId":"` + b.RequestID + `","envCode":"rdev.ali","objectCode":"A00003","workloadKey":"opaque-workload","clusterId":"cluster","namespace":"raptor-test","name":"rdev-test-client-client","uid":"app-uid"` + tc.fields + `}`
			result := `{"evidenceVersion":2,"requestId":"` + b.RequestID + `","attemptId":"` + b.AttemptID + `","selectedModel":"gpt-6.1-sol","actualModel":"gpt-6.1-sol","selectedProvider":"codex","actualProvider":"openai-codex","answer":"Two ready replicas.","generatedAt":"2026-11-10T06:04:47Z","evidence":[
				{"server":"raptor","tool":"request_get","observedAt":"2026-11-10T06:04:42Z","evidenceMode":"catalog","identity":{"requestId":"` + b.RequestID + `","envCode":"rdev.ali","objectCode":"A00003"},"state":{"available":true}},
				{"server":"infra","tool":"deployments_list","observedAt":"2026-11-10T06:04:43Z","evidenceMode":"discovery","identity":` + identity + `,"state":{"state":"ready"}},
				{"server":"infra","tool":"deployment_status_get","observedAt":"2026-11-10T06:04:46Z","evidenceMode":"live","identity":` + identity + `,"state":{"readyReplicas":2}}
			]}`
			read := func(_ context.Context, path string, _ int64) ([]byte, error) {
				if path != "/tmp/raptor-private/conversation-turn-"+turnID+".json" {
					t.Fatalf("unexpected AX turn path %q", path)
				}
				return []byte(`{"turnId":"` + turnID + `","messageIds":[],"result":` + result + `}`), nil
			}
			turns, err := observeConversation(context.Background(), events, b, read, nil)
			if (err == nil) != tc.want || tc.want && len(turns) != 1 {
				t.Errorf("conversation turn count %d, error %v", len(turns), err)
			}
			_, err = decodeTerminal([]byte(`{"phase":"live-inference","passed":true,"result":`+result+`}`), b, nil)
			if (err == nil) != tc.want {
				t.Errorf("terminal result error %v", err)
			}
		})
	}
}
