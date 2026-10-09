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
