package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"path"
	"regexp"
	"strings"
)

// ObserverSQLSource reads a controller-selected private SSE-OSS object, never
// an agent-selected key. Passwords stay outside Terraform state and sessions.
type ObserverSQLSource struct {
	Objects                            ObjectReader
	Key, EnvCode, DBCode, DBInstanceID string
}

func (s ObserverSQLSource) Load(ctx context.Context, in execution.LiveStart) (ObserverSQLCredential, error) {
	bad := func() (ObserverSQLCredential, error) { return ObserverSQLCredential{}, ErrRuntimeUnavailable }
	scope := in.Access.ReplacementScope
	if ctx.Err() != nil || s.Objects == nil || s.Key == "" || path.Clean(s.Key) != s.Key || strings.HasPrefix(s.Key, "/") || strings.Contains(s.Key, "..") || scope == nil || s.EnvCode != "rdev.ali" || s.EnvCode != in.Binding.EnvCode || scope.EnvCode != s.EnvCode || scope.TargetDBCode != s.DBCode || s.DBInstanceID == "" {
		return ObserverSQLCredential{}, ErrConfiguration
	}
	algorithm, err := s.Objects.BucketEncryption(ctx)
	if err != nil || algorithm != "AES256" {
		return bad()
	}
	meta, err := s.Objects.Head(ctx, s.Key)
	if err != nil || meta.Encryption != "AES256" || meta.Bytes < 1 || meta.Bytes > 65536 {
		return bad()
	}
	reader, err := s.Objects.Read(ctx, s.Key)
	if err != nil {
		return bad()
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 65537))
	if err != nil || int64(len(raw)) != meta.Bytes {
		return bad()
	}
	var value struct {
		Version      int    `json:"version"`
		EnvCode      string `json:"envCode"`
		DBCode       string `json:"targetDbCode"`
		DBInstanceID string `json:"dbInstanceId"`
		User         string `json:"user"`
		Password     string `json:"password"`
		Database     string `json:"database"`
		SSLMode      string `json:"sslmode"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil {
		return bad()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return bad()
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	if value.Version != 1 || value.EnvCode != s.EnvCode || value.DBCode != s.DBCode || value.DBInstanceID != s.DBInstanceID || !valid.MatchString(value.User) || !valid.MatchString(value.Database) || len(value.Password) < 1 || len(value.Password) > 4096 || (value.SSLMode != "disable" && value.SSLMode != "verify-full") || ctx.Err() != nil {
		return bad()
	}
	return ObserverSQLCredential{User: value.User, Password: value.Password, Database: value.Database, SSLMode: value.SSLMode}, nil
}
