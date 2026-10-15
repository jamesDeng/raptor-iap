package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRecognizableAndKnownCredentialsNeverPersist(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id := NewID()
	s.Receive(ctx, id)
	for _, value := range []string{`{"access_token":"synthetic-token-value"}`, `{"api_key":"synthetic-key-value"}`, `{"client_secret":"synthetic-secret-value"}`, "https://synthetic:password@host/path"} {
		details, _ := json.Marshal(map[string]string{"result": value})
		if e := s.AppendEvent(ctx, ProgressEvent{EventID: NewID(), RequestID: id, Kind: "tool_result", Summary: "Result", Details: details, EvidenceMode: "simulated"}); e != nil {
			t.Fatal(e)
		}
	}
	events, e := s.Events(ctx, id, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range events {
		if strings.Contains(string(v.Details), "synthetic") {
			t.Fatal("credential persisted", string(v.Details))
		}
	}
}

func TestKnownValueRedaction(t *testing.T) {
	s := testStore(t)
	s.KnownSecrets = []string{"synthetic-opaque-value"}
	ctx := context.Background()
	id := NewID()
	s.Receive(ctx, id)
	details, _ := json.Marshal(map[string]string{"result": "output synthetic-opaque-value here"})
	if e := s.AppendEvent(ctx, ProgressEvent{EventID: NewID(), RequestID: id, Kind: "tool_result", Summary: "synthetic-opaque-value", Details: details, EvidenceMode: "simulated"}); e != nil {
		t.Fatal(e)
	}
	v, _ := s.Events(ctx, id, 0)
	if strings.Contains(v[0].Summary+string(v[0].Details), "synthetic-opaque-value") {
		t.Fatal("known value persisted")
	}
}

func TestIncomingInstructionCanMentionCredentialsWithoutPersistingSecrets(t *testing.T) {
	question := "Read-only recovery smoke check for poc-traffic-client (A00003) in rdev.ali. Inspect its current deployment readiness and database/proxy dependencies using the available read-only tools. Report verified findings and any access limitations. Do not restart, deploy, modify, create or delete infrastructure, change catalog metadata, or expose credentials."
	input, err := json.Marshal(ExecutionInput{RequestID: NewID(), Definition: json.RawMessage(`{"operation":"application.question","question":` + string(mustJSON(t, question)) + `}`)})
	if err != nil {
		t.Fatal(err)
	}
	if containsSensitive(string(input), nil) {
		t.Fatal("benign instruction was treated as credential material")
	}
	if containsSensitive("No credentials were exposed; access limitations were reported.", nil) {
		t.Fatal("benign result was treated as credential material")
	}
	for _, raw := range []string{
		`{"question":"Bearer synthetic-token-value"}`,
		`{"question":"password=synthetic-value"}`,
		`{"credential":"synthetic-value"}`,
		`{"question":"sk-abcdefghijklmnop"}`,
	} {
		if !containsSensitive(raw, nil) {
			t.Fatalf("credential material accepted: %s", raw)
		}
	}
	if !containsSensitive(`{"question":"opaque-secret-value"}`, []string{"opaque-secret-value"}) {
		t.Fatal("configured secret accepted")
	}
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
