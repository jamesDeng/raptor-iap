package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

func TestPostDrainTerminalSessionMustExtendTheSameProof(t *testing.T) {
	anchor := []byte("{\"type\":\"session\",\"id\":\"saved\"}\n{\"type\":\"message\"}\n")
	proof := pgcatDrainEvidence{DrainActionID: "accepted-drain"}
	record, _ := json.Marshal(map[string]any{"type": "custom", "customType": "raptor-pgcat-drain-v1", "data": map[string]any{"proof": proof}})
	terminal := append(append([]byte{}, anchor...), append(record, '\n')...)
	sum := sha256.Sum256(terminal)
	got, err := verifyPostDrainSession(anchor, terminal, "saved", proof)
	if err != nil || got != hex.EncodeToString(sum[:]) {
		t.Fatal("verified extension rejected or wrong digest returned", err)
	}
	for _, tc := range []struct {
		name, id string
		data     []byte
		proof    pgcatDrainEvidence
	}{
		{"foreign session", "wrong-session", terminal, proof},
		{"different drain", "saved", terminal, pgcatDrainEvidence{DrainActionID: "other"}},
		{"no drain proof", "saved", anchor, proof},
		{"rewritten anchor", "saved", bytes.Replace(terminal, []byte(`"message"`), []byte(`"model"`), 1), proof},
		{"duplicate drain proof", "saved", bytes.Join([][]byte{terminal, record, []byte("\n")}, nil), proof},
		{"invalid appended entry", "saved", bytes.Join([][]byte{terminal, []byte("invalid JSON\n")}, nil), proof},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := verifyPostDrainSession(anchor, tc.data, tc.id, tc.proof); err == nil {
				t.Fatal("invalid session accepted")
			}
		})
	}
}

func TestPostDrainSessionFileReturnsOnlyTheExactRegularEntry(t *testing.T) {
	const session = "saved-session\n"
	for _, name := range []string{"valid", "duplicate", "link", "wrong path", "digest changed", "truncated gzip", "expanded too large"} {
		t.Run(name, func(t *testing.T) {
			var raw bytes.Buffer
			gz := gzip.NewWriter(&raw)
			writer := tar.NewWriter(gz)
			write := func(entry, contents string, kind byte) {
				t.Helper()
				if err := writer.WriteHeader(&tar.Header{Name: entry, Typeflag: kind, Size: int64(len(contents)), Linkname: "auth.json"}); err != nil {
					t.Fatal(err)
				}
				if _, err := writer.Write([]byte(contents)); err != nil {
					t.Fatal(err)
				}
			}
			write("auth.json", "private-credential-fixture", tar.TypeReg)
			switch name {
			case "link":
				write("sessions/saved.jsonl", "", tar.TypeSymlink)
			case "wrong path":
				write("sessions/sub/saved.jsonl", session, tar.TypeReg)
			default:
				write("sessions/saved.jsonl", session, tar.TypeReg)
			}
			if name == "duplicate" {
				write("sessions/saved.jsonl", session, tar.TypeReg)
			}
			if name == "expanded too large" {
				write("sessions/other.jsonl", strings.Repeat("x", 17*1024*1024), tar.TypeReg)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if name == "truncated gzip" {
				raw.Truncate(raw.Len() - 1)
			}
			sum := sha256.Sum256(raw.Bytes())
			ref := execution.VerifiedCheckpoint{ArchiveKey: "auth/lifecycle/attempt.tgz", Bytes: int64(raw.Len()), SHA256: hex.EncodeToString(sum[:]), VerifiedAt: time.Now()}
			if name == "digest changed" {
				ref.SHA256 = strings.Repeat("0", 64)
			}
			v := CheckpointVerifier{Objects: fixtureObjects{archive: raw.String()}, Prefix: "auth"}
			out, err := v.postDrainSessionFile(context.Background(), ref, "saved.jsonl")
			if name == "valid" {
				if err != nil || string(out) != session {
					t.Fatal("exact regular session not returned", err)
				}
			} else if err == nil || len(out) != 0 {
				t.Fatal("invalid archive yielded a session")
			}
		})
	}
}
