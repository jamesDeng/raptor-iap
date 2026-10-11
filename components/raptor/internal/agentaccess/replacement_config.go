package agentaccess

import (
	"bytes"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"io"
)

func DecodeReplacementScope(reader io.Reader) (ReplacementScope, error) {
	var scope ReplacementScope
	raw, err := io.ReadAll(io.LimitReader(reader, 16385))
	if err != nil || len(raw) > 16384 {
		return ReplacementScope{}, domain.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&scope) != nil {
		return ReplacementScope{}, domain.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ReplacementScope{}, domain.ErrInvalid
	}
	binding := Binding{Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: scope.ProxyCode, EnvCode: scope.EnvCode, ClusterID: scope.Application.ClusterID}
	if scope.EnvCode != "rdev.ali" || !scope.ValidFor(binding) {
		return ReplacementScope{}, domain.ErrInvalid
	}
	return scope, nil
}
