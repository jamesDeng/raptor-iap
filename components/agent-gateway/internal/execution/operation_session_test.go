package execution

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOperationSessionJournalFencesIdentityAndOwner(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.DefinitionSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: NewID(), SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("a", 64), Archive: VerifiedCheckpoint{ArchiveKey: "requests/a.tgz", ChecksumKey: "requests/a.sha256", SHA256: strings.Repeat("b", 64), Bytes: 10, PiVersion: "0.99.2", Encryption: "AES256", VerifiedAt: time.Now()}}
	if e := s.SaveOperationSession(ctx, x.AttemptID, "owner", cp); e != nil {
		t.Fatal(e)
	}
	saved, e := s.OperationSession(ctx, b)
	if e != nil || saved == nil || saved.SessionID != cp.SessionID {
		t.Fatal("durable session missing", e)
	}
	foreign := b
	foreign.SkillsCommit = strings.Repeat("c", 40)
	if _, e = s.OperationSession(ctx, foreign); e == nil {
		t.Fatal("foreign skills reused history")
	}
	if e = s.SaveOperationSession(ctx, x.AttemptID, "stale", cp); e == nil {
		t.Fatal("stale owner wrote history")
	}
	cp.RequestID = NewID()
	if e = s.SaveOperationSession(ctx, x.AttemptID, "owner", cp); e == nil {
		t.Fatal("foreign request persisted")
	}
}
