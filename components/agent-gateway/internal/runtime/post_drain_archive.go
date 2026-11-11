package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path"
	"reflect"
	"strings"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

const postDrainArchiveLimit = 16 * 1024 * 1024

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// VerifyCheckpoint authenticates the archive reference before this second read.
// Recheck the exact bytes before inspecting the one request-bound session file.
func (v CheckpointVerifier) postDrainSessionFile(ctx context.Context, archive execution.VerifiedCheckpoint, file string) ([]byte, error) {
	if v.Objects == nil || v.Prefix == "" || !strings.HasPrefix(archive.ArchiveKey, v.Prefix+"/") ||
		path.Clean(archive.ArchiveKey) != archive.ArchiveKey || archive.VerifiedAt.IsZero() ||
		archive.Bytes < 1 || archive.Bytes > postDrainArchiveLimit ||
		path.Base(file) != file || !strings.HasSuffix(file, ".jsonl") || strings.ContainsAny(file, "\\\x00") {
		return nil, ErrCheckpointVerification
	}
	object, err := v.Objects.Read(ctx, archive.ArchiveKey)
	if err != nil {
		return nil, ErrCheckpointVerification
	}
	compressed, err := bounded(object, archive.Bytes)
	object.Close()
	if err != nil || int64(len(compressed)) != archive.Bytes || digestBytes(compressed) != archive.SHA256 {
		return nil, ErrCheckpointVerification
	}
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, ErrCheckpointVerification
	}
	defer gz.Close()
	// The producer allows 16 MiB of files plus 1 MiB of tar headers and padding.
	expanded := &io.LimitedReader{R: gz, N: postDrainArchiveLimit + 1024*1024 + 1}
	tarball := tar.NewReader(expanded)
	seen := make(map[string]bool)
	var session []byte
	var filesSize int64
	for {
		entry, err := tarball.Next()
		if err == io.EOF {
			break
		}
		if err != nil || len(seen) == 1024 || entry.Size < 0 || entry.Size > postDrainArchiveLimit-filesSize {
			return nil, ErrCheckpointVerification
		}
		name := strings.TrimSuffix(entry.Name, "/")
		regular := entry.Typeflag == tar.TypeReg
		if path.Clean(name) != name || seen[name] || (!regular && entry.Typeflag != tar.TypeDir) ||
			!(name == "auth.json" || name == "sessions" || strings.HasPrefix(name, "sessions/")) {
			return nil, ErrCheckpointVerification
		}
		seen[name] = true
		filesSize += entry.Size
		if name == "sessions/"+file {
			if !regular || entry.Name != name || entry.Size == 0 {
				return nil, ErrCheckpointVerification
			}
			session, err = bounded(tarball, entry.Size)
			if err != nil || int64(len(session)) != entry.Size {
				return nil, ErrCheckpointVerification
			}
		}
	}
	// Read through the gzip checksum. Tar may leave zero padding after its end.
	trailing, err := io.ReadAll(expanded)
	if err != nil || expanded.N == 0 || len(bytes.Trim(trailing, "\x00")) != 0 || len(session) == 0 || ctx.Err() != nil {
		return nil, ErrCheckpointVerification
	}
	return session, nil
}

func verifyPostDrainSession(anchor, terminal []byte, sessionID string, proof pgcatDrainEvidence) (string, error) {
	if sessionID == "" || proof.DrainActionID == "" || len(anchor) == 0 || len(terminal) <= len(anchor) ||
		len(terminal) > postDrainArchiveLimit || anchor[len(anchor)-1] != '\n' || terminal[len(terminal)-1] != '\n' ||
		!bytes.HasPrefix(terminal, anchor) {
		return "", ErrCheckpointVerification
	}
	header, lines, _ := bytes.Cut(terminal, []byte{'\n'})
	var fields map[string]json.RawMessage
	var typ, id string
	if json.Unmarshal(header, &fields) != nil || json.Unmarshal(fields["type"], &typ) != nil || typ != "session" ||
		json.Unmarshal(fields["id"], &id) != nil || id != sessionID {
		return "", ErrCheckpointVerification
	}
	wantJSON, err := json.Marshal(proof)
	if err != nil {
		return "", ErrCheckpointVerification
	}
	decodeProof := func(raw []byte) (any, error) {
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		err := dec.Decode(&decoded)
		return decoded, err
	}
	want, err := decodeProof(wantJSON)
	if err != nil {
		return "", ErrCheckpointVerification
	}
	found := false
	for len(lines) != 0 {
		var line []byte
		start := len(terminal) - len(lines)
		line, lines, _ = bytes.Cut(lines, []byte{'\n'})
		fields = nil
		typ = ""
		if json.Unmarshal(line, &fields) != nil || json.Unmarshal(fields["type"], &typ) != nil || typ == "" || typ == "session" {
			return "", ErrCheckpointVerification
		}
		var customType string
		if typ != "custom" || json.Unmarshal(fields["customType"], &customType) != nil || customType != "raptor-pgcat-drain-v1" {
			continue
		}
		var data map[string]json.RawMessage
		if found || start < len(anchor) || json.Unmarshal(fields["data"], &data) != nil {
			return "", ErrCheckpointVerification
		}
		actual, err := decodeProof(data["proof"])
		if err != nil || !reflect.DeepEqual(actual, want) {
			return "", ErrCheckpointVerification
		}
		found = true
	}
	if !found {
		return "", ErrCheckpointVerification
	}
	return digestBytes(terminal), nil
}
