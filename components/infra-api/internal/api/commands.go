package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/jsoninput"
	"reflect"
	"strings"
	"time"
)

type Commander interface {
	Restart(context.Context, domain.Environment, domain.RestartCommand) (domain.RestartReceipt, error)
	Scale(context.Context, domain.Environment, domain.ScaleCommand) (domain.ScaleReceipt, error)
	SetProtection(context.Context, domain.Environment, domain.ProtectionCommand) (domain.NodeReceipt, error)
	Deregister(context.Context, domain.Environment, domain.DeregisterCommand) (domain.NodeReceipt, error)
}

// Authorizer must verify the caller's request binding outside model arguments.
// principal is the authenticated Basic Auth username, not a submitted selector.
type Authorizer interface {
	Authorize(context.Context, string, string, any) error
}

// Visibility only controls the catalog; Authorize remains authoritative on calls.
type CommandToolVisibility interface{ ExposeCommands() bool }

type commandSpec struct {
	name, path string
	sample     any
}

var commandSpecs = []commandSpec{
	{"deployment_restart", "/v1/deployment-restart", domain.RestartCommand{}},
	{"db_proxy_scale", "/v1/db-proxy-scale", domain.ScaleCommand{}},
	{"db_proxy_node_protection_set", "/v1/db-proxy-node-protection", domain.ProtectionCommand{}},
	{"db_proxy_nodes_deregister", "/v1/db-proxy-node-deregistration", domain.DeregisterCommand{}},
}

func commandPath(path string) bool {
	for _, s := range commandSpecs {
		if s.path == path {
			return true
		}
	}
	return false
}

func strictJSON(raw []byte, out any) error { return jsoninput.Decode(raw, out) }
func textOK(s string) bool                 { return strings.TrimSpace(s) != "" && len(s) <= 256 }
func nodesOK(ids []string) bool {
	if len(ids) == 0 || len(ids) > 50 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !textOK(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func proxyOK(c domain.ProxyTarget) bool {
	return textOK(c.RequestID) && textOK(c.EnvCode) && textOK(c.ProxyCode) && textOK(c.GroupID)
}
func (o operations) command(ctx context.Context, path string, raw []byte) (any, error) {
	bad := func() (any, error) { return nil, &operationError{400, "InvalidInput"} }
	var in any
	var envCode string
	switch path {
	case "/v1/deployment-restart":
		var c domain.RestartCommand
		if strictJSON(raw, &c) != nil {
			return bad()
		}
		for _, s := range []string{c.RequestID, c.EnvCode, c.AppCode, c.ClusterID, c.Namespace, c.Name, c.UID} {
			if !textOK(s) {
				return bad()
			}
		}
		in = c
		envCode = c.EnvCode
	case "/v1/db-proxy-scale":
		var c domain.ScaleCommand
		if strictJSON(raw, &c) != nil || !proxyOK(c.ProxyTarget) || !textOK(c.ActionID) || c.DesiredCapacity < 0 {
			return bad()
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if fields["desiredCapacity"] == nil || bytes.Equal(fields["desiredCapacity"], []byte("null")) {
			return bad()
		}
		in = c
		envCode = c.EnvCode
	case "/v1/db-proxy-node-protection":
		var c domain.ProtectionCommand
		if strictJSON(raw, &c) != nil || !proxyOK(c.ProxyTarget) || !nodesOK(c.InstanceIDs) {
			return bad()
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if fields["protected"] == nil || bytes.Equal(fields["protected"], []byte("null")) {
			return bad()
		}
		in = c
		envCode = c.EnvCode
	case "/v1/db-proxy-node-deregistration":
		var c domain.DeregisterCommand
		if strictJSON(raw, &c) != nil || !proxyOK(c.ProxyTarget) || !textOK(c.ServerGroupID) || !nodesOK(c.InstanceIDs) {
			return bad()
		}
		in = c
		envCode = c.EnvCode
	default:
		return nil, &operationError{404, "NotFound"}
	}
	env, ok := o.scopes[envCode]
	if !ok {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if o.authorizer == nil || o.authorizer.Authorize(ctx, o.principal, path, in) != nil {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if o.commander == nil {
		return nil, &operationError{503, "NotConfigured"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var data any
	var e error
	switch c := in.(type) {
	case domain.RestartCommand:
		data, e = o.commander.Restart(ctx, env, c)
	case domain.ScaleCommand:
		data, e = o.commander.Scale(ctx, env, c)
	case domain.ProtectionCommand:
		data, e = o.commander.SetProtection(ctx, env, c)
	case domain.DeregisterCommand:
		data, e = o.commander.Deregister(ctx, env, c)
	}
	if e != nil {
		return nil, commandFailure(e)
	}
	return data, nil
}
func commandFailure(e error) *operationError {
	if errors.Is(e, domain.ErrScope) {
		return &operationError{403, "ScopeMismatch"}
	}
	if errors.Is(e, domain.ErrIdentity) {
		return &operationError{409, "IdentityChanged"}
	}
	if errors.Is(e, domain.ErrNotConfigured) {
		return &operationError{503, "NotConfigured"}
	}
	var ce *domain.CommandError
	if errors.As(e, &ce) {
		switch ce.Code {
		case "SubmissionUnknown":
			return &operationError{503, ce.Code}
		case "TargetChanged", "InvalidCapacity", "AmbiguousTarget", "TargetNotFound", "NoEligibleNodes", "ConnectedClientsPresent", "InsufficientHealthyCapacity", "ApprovalRequired":
			return &operationError{409, ce.Code}
		case "MetricsUnavailable", "MetricsInvalid":
			return &operationError{503, ce.Code}
		}
	}
	return &operationError{503, "ProviderUnavailable"}
}

// Schema generated from the typed input has all fields required, including false
// and zero values. Embedded target fields remain flat on both transports.
func commandSchema(sample any) map[string]any {
	props := map[string]any{}
	required := []string{}
	var fields func(reflect.Type)
	fields = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous {
				fields(f.Type)
				continue
			}
			name := f.Tag.Get("json")
			p := map[string]any{}
			switch f.Type.Kind() {
			case reflect.String:
				p["type"] = "string"
				p["minLength"] = 1
				p["maxLength"] = 256
			case reflect.Int:
				p["type"] = "integer"
				p["minimum"] = 0
			case reflect.Bool:
				p["type"] = "boolean"
			case reflect.Slice:
				p["type"] = "array"
				p["items"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 256}
				p["minItems"] = 1
				p["maxItems"] = 50
				p["uniqueItems"] = true
			}
			props[name] = p
			required = append(required, name)
		}
	}
	fields(reflect.TypeOf(sample))
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
