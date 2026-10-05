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
