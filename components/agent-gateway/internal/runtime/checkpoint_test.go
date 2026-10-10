package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"strings"
	"testing"
	"time"
)

type fixtureObjects struct {
	archive, checksum, encryption string
	size                          int64
	missing                       bool
}

func (f fixtureObjects) BucketEncryption(context.Context) (string, error) { return "AES256", nil }
func (f fixtureObjects) Head(_ context.Context, key string) (ObjectMeta, error) {
	if f.missing {
		return ObjectMeta{}, ErrRuntimeUnavailable
	}
	n := f.size
	if strings.HasSuffix(key, ".sha256") {
		n = int64(len(f.checksum))
	}
	return ObjectMeta{Bytes: n, Encryption: f.encryption}, nil
}
func (f fixtureObjects) Read(_ context.Context, key string) (io.ReadCloser, error) {
	v := f.archive
	if strings.HasSuffix(key, ".sha256") {
		v = f.checksum
	}
	return io.NopCloser(strings.NewReader(v)), nil
}
func TestCheckpointVerifierRequiresRemoteEncryptedContent(t *testing.T) {
	sum := sha256.Sum256([]byte("archive"))
	hash := hex.EncodeToString(sum[:])
	ref := execution.VerifiedCheckpoint{ArchiveKey: "auth/lifecycle/g.tgz", ChecksumKey: "auth/lifecycle/g.sha256", SHA256: hash, Bytes: 7, PiVersion: "0.99.2"}
	good := fixtureObjects{archive: "archive", checksum: hash, encryption: "AES256", size: 7}
	for _, name := range []string{"good", "encryption", "size", "hash", "checksum", "prefix", "missing", "partial"} {
		t.Run(name, func(t *testing.T) {
			f, r := good, ref
			switch name {
			case "encryption":
				f.encryption = ""
			case "size":
				f.size = 8
			case "hash":
				f.archive = "changed"
			case "checksum":
				f.checksum = strings.Repeat("0", 64)
			case "prefix":
				r.ArchiveKey = "other/g.tgz"
			case "missing":
				f.missing = true
			case "partial":
				f.archive = "arc"
			}
			v := CheckpointVerifier{Objects: f, Prefix: "auth", Now: func() time.Time { return time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC) }}
			out, e := v.VerifyCheckpoint(context.Background(), r)
			if name == "good" {
				if e != nil || out.Encryption != "AES256" || out.VerifiedAt.IsZero() {
					t.Fatal(out, e)
				}
			} else if e == nil {
				t.Fatal("invalid checkpoint selected")
			}
		})
	}
}
func TestOperationCheckpointVerificationBindsRequest(t *testing.T) {
	sum := sha256.Sum256([]byte("archive"))
	hash := hex.EncodeToString(sum[:])
	v := CheckpointVerifier{Objects: fixtureObjects{archive: "archive", checksum: hash, encryption: "AES256", size: 7}, Prefix: "requests"}
	b := execution.AttemptBinding{RequestID: "request", DefinitionSHA256: strings.Repeat("a", 64), SkillsCommit: strings.Repeat("b", 40), Model: "gpt-5.6-luna"}
	c := execution.SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("c", 64), Archive: execution.VerifiedCheckpoint{ArchiveKey: "requests/g.tgz", ChecksumKey: "requests/g.sha256", SHA256: hash, Bytes: 7, PiVersion: "0.99.2"}}
	out, e := v.VerifyOperationCheckpoint(context.Background(), c, b)
	if e != nil || out.Archive.Encryption != "AES256" {
		t.Fatal("valid session checkpoint refused", e)
	}
	c.RequestID = "foreign"
	if _, e = v.VerifyOperationCheckpoint(context.Background(), c, b); e == nil {
		t.Fatal("foreign request checkpoint accepted")
	}
}
