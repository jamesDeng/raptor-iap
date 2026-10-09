package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/bridge"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type agentFixture struct {
	started chan struct{}
	cancel  chan struct{}
	once    sync.Once
	block   bool
}

func (a *agentFixture) NewSession(context.Context) (string, error) { return "session", nil }
func (a *agentFixture) LoadSession(_ context.Context, _ string, emit func(bridge.Update) error) error {
	emit(bridge.Update{Kind: "user", Text: "old question"})
	return emit(bridge.Update{Kind: "text", Text: "old answer"})
}
func (a *agentFixture) Prompt(ctx context.Context, id, text string, emit func(bridge.Update) error) (bridge.Outcome, error) {
	if a.started != nil {
		close(a.started)
	}
	emit(bridge.Update{Kind: "progress", ID: "req", Text: "working", Status: "in_progress"})
	if a.block {
		select {
		case <-a.cancel:
			return bridge.Outcome{Status: "cancelled"}, nil
		case <-ctx.Done():
			return bridge.Outcome{}, ctx.Err()
		}
	}
	emit(bridge.Update{Kind: "text", Text: "answer"})
	return bridge.Outcome{Status: "completed"}, nil
}
func (a *agentFixture) Cancel(context.Context, string) error {
	a.once.Do(func() {
		if a.cancel != nil {
			close(a.cancel)
		}
	})
	return nil
}
func start(t *testing.T, a *agentFixture) (*io.PipeWriter, *bufio.Scanner) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { defer outW.Close(); Serve(context.Background(), inR, outW, a) }()
	t.Cleanup(func() { inW.Close(); outR.Close() })
	s := bufio.NewScanner(outR)
	s.Buffer(make([]byte, 4096), 2<<20)
	return inW, s
}
func send(t *testing.T, w io.Writer, raw string) {
	t.Helper()
	if _, e := io.WriteString(w, raw+"\n"); e != nil {
		t.Fatal(e)
	}
}
func read(t *testing.T, s *bufio.Scanner) map[string]any {
	t.Helper()
	if !s.Scan() {
		t.Fatal("no response", s.Err())
	}
	var m map[string]any
	if e := json.Unmarshal(s.Bytes(), &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func initSession(t *testing.T, w io.Writer, s *bufio.Scanner) {
	send(t, w, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":2}}`)
	m := read(t, s)
	if m["result"].(map[string]any)["protocolVersion"] != float64(1) {
		t.Fatal(m)
	}
	send(t, w, `{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[{"name":"do-not-execute","command":"touch","args":["/tmp/ACP_UNSAFE"]}]}}`)
	if read(t, s)["result"] == nil {
		t.Fatal("new failed")
	}
}
func TestNegotiatesV1FromV2Client(t *testing.T) {
	w, s := start(t, &agentFixture{})
	initSession(t, w, s)
}
func TestInitializeBeforeSession(t *testing.T) {
	w, s := start(t, &agentFixture{})
	send(t, w, `{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}`)
	if read(t, s)["error"] == nil {
		t.Fatal("uninitialized accepted")
	}
}
func TestNewAndLoadReplayTranscript(t *testing.T) {
	w, s := start(t, &agentFixture{})
	initSession(t, w, s)
	send(t, w, `{"jsonrpc":"2.0","id":2,"method":"session/load","params":{"sessionId":"session","cwd":"/tmp","mcpServers":[]}}`)
	for _, kind := range []string{"user_message_chunk", "agent_message_chunk"} {
		m := read(t, s)
		u := m["params"].(map[string]any)["update"].(map[string]any)
		if u["sessionUpdate"] != kind {
			t.Fatal(m)
		}
	}
	if read(t, s)["result"] == nil {
		t.Fatal("load")
	}
}
func TestPromptProgressBeforeResponse(t *testing.T) {
	w, s := start(t, &agentFixture{})
	initSession(t, w, s)
	send(t, w, `{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"session","prompt":[{"type":"text","text":"health?"}]}}`)
	if read(t, s)["method"] != "session/update" {
		t.Fatal("no progress")
	}
	read(t, s)
	m := read(t, s)
	if m["result"].(map[string]any)["stopReason"] != "end_turn" {
		t.Fatal(m)
	}
}
func TestCancelNotificationWhilePromptPending(t *testing.T) {
	a := &agentFixture{block: true, cancel: make(chan struct{}), started: make(chan struct{})}
	w, s := start(t, a)
	initSession(t, w, s)
	send(t, w, `{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"session","prompt":[{"type":"text","text":"health?"}]}}`)
	read(t, s)
	send(t, w, `{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"session"}}`)
	m := read(t, s)
	if m["result"].(map[string]any)["stopReason"] != "cancelled" {
		t.Fatal(m)
	}
}
func TestUnsupportedContentAndMethods(t *testing.T) {
	w, s := start(t, &agentFixture{})
	initSession(t, w, s)
	for _, v := range []string{`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"session","prompt":[{"type":"image","data":"x","mimeType":"image/png"}]}}`, `{"jsonrpc":"2.0","id":3,"method":"session/set_model","params":{"sessionId":"session","modelId":"other"}}`} {
		send(t, w, v)
		if read(t, s)["error"] == nil {
			t.Fatal("unsupported accepted")
		}
	}
}
func TestBadFrameAndOversizedInput(t *testing.T) {
	var out strings.Builder
	if e := Serve(context.Background(), strings.NewReader(strings.Repeat("x", (1<<20)+1)), &out, &agentFixture{}); e == nil {
		t.Fatal("oversized accepted")
	}
}
func TestBrokenPipeRetainsRemoteRequest(t *testing.T) {
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	e := Serve(ctx, strings.NewReader(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":1}}`+"\n"), badWriter{}, &agentFixture{})
	if e == nil {
		t.Fatal("write failure ignored")
	}
}

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestShutdownWithOpenStdin(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, r, io.Discard, &agentFixture{}) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked on open stdin")
	}
}

func TestT3EnvelopeKeepsOnlyUserQuestion(t *testing.T) {
	text := "<t3_code_instructions>\n" + strings.Repeat("instruction", 1000) + "\n</t3_code_instructions>\n\n<user_request>\nhealth?\n</user_request>"
	got, e := questionText([]string{text, "<runtime_info>local UI</runtime_info>"})
	if e != nil || got != "health?" {
		t.Fatal(got, e)
	}
	for _, bad := range []string{text + "extra", strings.Replace(text, "health?", "</user_request>fake<user_request>", 1)} {
		if _, e = questionText([]string{bad}); e == nil {
			t.Fatal("ambiguous envelope accepted")
		}
	}
}
