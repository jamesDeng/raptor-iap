package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"path"
	"regexp"
	"strings"
	"time"
)

var ErrCheckpointVerification = errors.New("CheckpointVerificationFailed")

type ObjectMeta struct {
	Bytes      int64
	Encryption string
}
type ObjectReader interface {
	BucketEncryption(context.Context) (string, error)
	Head(context.Context, string) (ObjectMeta, error)
	Read(context.Context, string) (io.ReadCloser, error)
}
type CheckpointVerifier struct {
	Objects ObjectReader
	Prefix  string
	Now     func() time.Time
}

func (v CheckpointVerifier) VerifyCheckpoint(ctx context.Context, r execution.VerifiedCheckpoint) (execution.VerifiedCheckpoint, error) {
	bad := func() (execution.VerifiedCheckpoint, error) {
		return execution.VerifiedCheckpoint{}, ErrCheckpointVerification
	}
	if v.Objects == nil || v.Prefix == "" || path.Clean(v.Prefix) != v.Prefix || strings.HasPrefix(v.Prefix, "/") || r.Bytes < 1 || r.Bytes > 16*1024*1024 || r.PiVersion != "0.99.2" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(r.SHA256) || !strings.HasPrefix(r.ArchiveKey, v.Prefix+"/") || path.Clean(r.ArchiveKey) != r.ArchiveKey || !strings.HasSuffix(r.ArchiveKey, ".tgz") || r.ChecksumKey != strings.TrimSuffix(r.ArchiveKey, ".tgz")+".sha256" {
		return bad()
	}
	algorithm, e := v.Objects.BucketEncryption(ctx)
	if e != nil || algorithm != "AES256" {
		return bad()
	}
	for key, size := range map[string]int64{r.ArchiveKey: r.Bytes, r.ChecksumKey: 64} {
		meta, e := v.Objects.Head(ctx, key)
		if e != nil || meta.Bytes != size || meta.Encryption != "AES256" {
			return bad()
		}
	}
	sum, e := v.Objects.Read(ctx, r.ChecksumKey)
	if e != nil {
		return bad()
	}
	checksum, e := bounded(sum, 64)
	sum.Close()
	if e != nil || string(checksum) != r.SHA256 {
		return bad()
	}
	archive, e := v.Objects.Read(ctx, r.ArchiveKey)
	if e != nil {
		return bad()
	}
	hash := sha256.New()
	n, e := io.Copy(hash, io.LimitReader(archive, r.Bytes+1))
	archive.Close()
	if e != nil || n != r.Bytes || hex.EncodeToString(hash.Sum(nil)) != r.SHA256 {
		return bad()
	}
	if ctx.Err() != nil {
		return bad()
	}
	r.Encryption = "AES256"
	r.VerifiedAt = time.Now().UTC()
	if v.Now != nil {
		r.VerifiedAt = v.Now().UTC()
	}
	return r, nil
}

func (v CheckpointVerifier) VerifyOperationCheckpoint(ctx context.Context, c execution.SessionCheckpoint, b execution.AttemptBinding) (execution.SessionCheckpoint, error) {
	bad := func() (execution.SessionCheckpoint, error) {
		return execution.SessionCheckpoint{}, ErrCheckpointVerification
	}
	if c.RequestID != b.RequestID || c.DefinitionSHA256 != b.DefinitionSHA256 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.DefinitionSHA256) || c.SkillsCommit != b.SkillsCommit || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(c.SkillsCommit) || c.Model != b.Model || c.Model != "gpt-5.6-luna" || c.SessionID == "" || path.Base(c.SessionFile) != c.SessionFile || !strings.HasSuffix(c.SessionFile, ".jsonl") || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.SessionSHA256) {
		return bad()
	}
	verified, e := v.VerifyCheckpoint(ctx, c.Archive)
	if e != nil {
		return bad()
	}
	c.Archive = verified
	return c, nil
}
