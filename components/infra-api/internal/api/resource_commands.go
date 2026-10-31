package api

import (
	"context"
	"encoding/json"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/resources"
	"reflect"
	"time"
)

type resourceProxyTarget struct {
	RequestID string `json:"requestId"`
	EnvCode   string `json:"envCode"`
	ProxyCode string `json:"proxyCode"`
}
type resourceScale struct {
	resourceProxyTarget
	ActionID        string `json:"actionId"`
	DesiredCapacity int    `json:"desiredCapacity"`
}
type resourceProtection struct {
	resourceProxyTarget
	NodeIDs   []string `json:"nodeIds"`
	Protected bool     `json:"protected"`
}
type resourceDeregister struct {
	resourceProxyTarget
	NodeIDs []string `json:"nodeIds"`
}
type resourceRestart struct {
	RequestID   string `json:"requestId"`
	EnvCode     string `json:"envCode"`
	AppCode     string `json:"appCode"`
	WorkloadKey string `json:"workloadKey"`
}

var resourceCommandSpecs = []commandSpec{{"deployment_restart", "/v1/deployment-restart", resourceRestart{}}, {"db_proxy_scale", "/v1/db-proxy-scale", resourceScale{}}, {"db_proxy_node_protection_set", "/v1/db-proxy-node-protection", resourceProtection{}}, {"db_proxy_nodes_deregister", "/v1/db-proxy-node-deregistration", resourceDeregister{}}}

func (o resourceOperations) command(ctx context.Context, path string, raw []byte) (any, error) {
	bad := func() (any, error) { return nil, &operationError{400, "InvalidInput"} }
	var in any
	var tool, env, code, request, kind string
	selectors := map[string]string{}
	// Decode every field strictly before authorization or resolution; null scalars fail.
	var fields map[string]json.RawMessage
	if strictJSON(raw, &fields) != nil {
		return bad()
	}
	// encoding/json matches struct fields case-insensitively. Enforce the exact
	// public schema keys first, including case variants of otherwise valid names.
	var props map[string]any
	for _, spec := range resourceCommandSpecs {
		if spec.path == path {
			props = commandSchema(spec.sample)["properties"].(map[string]any)
			break
		}
	}
	if props == nil {
		return nil, &operationError{404, "NotFound"}
	}
	if len(fields) != len(props) {
		return bad()
	}
	for name := range fields {
		if _, ok := props[name]; !ok {
			return bad()
		}
	}
	for _, v := range fields {
		if string(v) == "null" {
			return bad()
		}
	}
	switch path {
	case "/v1/db-proxy-scale":
		var c resourceScale
		if strictJSON(raw, &c) != nil || c.DesiredCapacity < 0 || fields["desiredCapacity"] == nil || !textOK(c.ActionID) {
			return bad()
		}
		in = c
		env, code, request = c.EnvCode, c.ProxyCode, c.RequestID
		kind = "db-proxy"
	case "/v1/db-proxy-node-protection":
		var c resourceProtection
		if strictJSON(raw, &c) != nil || !nodesOK(c.NodeIDs) || fields["protected"] == nil {
			return bad()
		}
		in = c
		env, code, request = c.EnvCode, c.ProxyCode, c.RequestID
		kind = "db-proxy"
	case "/v1/db-proxy-node-deregistration":
		var c resourceDeregister
		if strictJSON(raw, &c) != nil || !nodesOK(c.NodeIDs) {
			return bad()
		}
		in = c
		env, code, request = c.EnvCode, c.ProxyCode, c.RequestID
		kind = "db-proxy"
	case "/v1/deployment-restart":
		var c resourceRestart
		if strictJSON(raw, &c) != nil || !textOK(c.WorkloadKey) {
			return bad()
		}
		in = c
		env, code, request = c.EnvCode, c.AppCode, c.RequestID
		kind = "application"
		selectors["workloadKey"] = c.WorkloadKey
	default:
		return nil, &operationError{404, "NotFound"}
	}
	if !resourceCode.MatchString(env) || !resourceCode.MatchString(code) || !textOK(request) {
		return bad()
	}
	for _, s := range resourceCommandSpecs {
		if s.path == path {
			tool = s.name
		}
	}
	selectors["envCode"] = env
	selectors["requestId"] = request
	if kind == "application" {
		selectors["appCode"] = code
	} else {
		selectors["proxyCode"] = code
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	identity, e := o.authorize(ctx, path, tool, selectors, in, true)
	if e != nil {
		return nil, e
	}
	if o.commander == nil {
		return nil, &operationError{503, "NotConfigured"}
	}
	r, e := o.resolver.Resolve(ctx, env, kind, code)
	if e != nil {
		return nil, commandFailure(e)
	}
	if !resolvedSelectors(r, env, kind, code, identity) {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if identity != nil {
		if e = o.matchProof(ctx, *identity, r); e != nil {
			return nil, e
		}
	}
	var data any
	if c, ok := in.(resourceRestart); ok {
		a, err := selectApplication(r, c.WorkloadKey, identity)
		if err != nil {
			return nil, err
		}
		t := a.Target
		data, e = o.commander.Restart(ctx, r.Environment, domain.RestartCommand{RequestID: request, EnvCode: t.EnvCode, AppCode: t.AppCode, ClusterID: t.ClusterID, Namespace: t.Namespace, Name: t.Name, UID: t.UID})
	} else {
		if r.Proxy == nil || r.Proxy.Code != r.Context.Code || r.Environment.Code != env {
			return nil, &operationError{409, "IdentityChanged"}
		}
		target := domain.ProxyTarget{RequestID: request, EnvCode: env, ProxyCode: r.Context.Code, GroupID: r.Proxy.GroupID}
		switch c := in.(type) {
		case resourceScale:
			data, e = o.commander.Scale(ctx, r.Environment, domain.ScaleCommand{ProxyTarget: target, ActionID: c.ActionID, DesiredCapacity: c.DesiredCapacity})
		case resourceProtection:
			data, e = o.commander.SetProtection(ctx, r.Environment, domain.ProtectionCommand{ProxyTarget: target, InstanceIDs: c.NodeIDs, Protected: c.Protected})
		case resourceDeregister:
			data, e = o.commander.Deregister(ctx, r.Environment, domain.DeregisterCommand{ProxyTarget: target, ServerGroupID: r.Proxy.ServerGroupID, InstanceIDs: c.NodeIDs})
		}
	}
	if e != nil {
		return nil, commandFailure(e)
	}
	return data, nil
}
func resourceIdentityProof(i AgentIdentity) (resources.Proof, error) {
	s := i.ReplacementScope
	if s == nil {
		return resources.Proof{}, &operationError{403, "ScopeMismatch"}
	}
	a := s.Application
	expected := resources.ResolutionIdentity{EnvCode: s.EnvCode, AccountID: s.AccountID, Region: s.Region, ClusterID: a.ClusterID, ProxyCode: s.ProxyCode, GroupID: s.GroupID, ServerGroupID: s.ServerGroupID, ListenerID: s.ListenerID, Port: s.Port, TargetDBCode: s.TargetDBCode, DatabaseResourceID: s.DatabaseResourceID, Application: resources.ApplicationIdentity{EnvCode: a.EnvCode, AppCode: a.AppCode, ClusterID: a.ClusterID, Namespace: a.Namespace, Name: a.Name, UID: a.UID}}
	digest, e := resources.Digest(expected)
	if e != nil || s.ResolutionDigest != digest {
		return resources.Proof{}, &operationError{409, "IdentityChanged"}
	}
	return resources.Proof{Identity: expected, ResolutionDigest: digest}, nil
}
func (o resourceOperations) matchProof(ctx context.Context, i AgentIdentity, r domain.ResolvedResource) error {
	expected, e := resourceIdentityProof(i)
	if e != nil {
		return e
	}
	s := i.ReplacementScope
	actual, e := o.resolver.ResolveProof(ctx, s.EnvCode, s.ProxyCode)
	if e != nil {
		return commandFailure(e)
	}
	if actual.ResolutionDigest != expected.ResolutionDigest || !reflect.DeepEqual(actual.Identity, expected.Identity) {
		return &operationError{409, "IdentityChanged"}
	}
	return compareResolvedProof(i, r)
}
func compareResolvedProof(i AgentIdentity, r domain.ResolvedResource) error {
	s := i.ReplacementScope
	a := s.Application
	if r.Environment.Code != s.EnvCode || r.Environment.AccountID != s.AccountID || r.Environment.Region != s.Region || (r.Context.Kind != "database" && r.Environment.ClusterID != a.ClusterID) {
		return &operationError{409, "IdentityChanged"}
	}
	if r.Context.Kind == "db-proxy" {
		p := r.Proxy
		if p == nil || p.Code != s.ProxyCode || p.GroupID != s.GroupID || p.ServerGroupID != s.ServerGroupID || p.ListenerID != s.ListenerID || p.Port != s.Port || p.TargetDBCode != s.TargetDBCode {
			return &operationError{409, "IdentityChanged"}
		}
	}
	if r.Context.Kind == "application" {
		if len(r.ApplicationWorkloads) != 1 {
			return &operationError{409, "IdentityChanged"}
		}
		if _, e := selectApplication(r, "", &i); e != nil {
			return e
		}
	}
	if r.Context.Kind == "database" && (len(r.DatabaseWorkloads) != 1 || r.DatabaseWorkloads[0].ResourceID != s.DatabaseResourceID) {
		return &operationError{409, "IdentityChanged"}
	}
	return nil
}

// permitsResource validates private token lifecycle and logical selectors only.
// The legacy handler deliberately continues using permits and cloud selectors.
func (i AgentIdentity) permitsResource(tool string, s map[string]string) bool {
	if !i.permits("", map[string]string{}) {
		return false
	}
	if tool == "" {
		return len(s) == 0
	}
	b := i.Binding
	replacement := b.Operation == "db-proxy.replace-nodes"
	mutation := false
	want := map[string]string{"envCode": b.EnvCode}
	switch tool {
	case "deployments_list":
		want["kind"] = b.ObjectKind
		want["code"] = b.ObjectCode
		if replacement {
			scope := i.ReplacementScope
			if s["kind"] == "application" && s["code"] == scope.Application.AppCode {
				want["kind"] = "application"
				want["code"] = scope.Application.AppCode
			}
			if s["kind"] == "database" && s["code"] == scope.TargetDBCode {
				want["kind"] = "database"
				want["code"] = scope.TargetDBCode
			}
		}
	case "deployment_status_get", "deployment_restart":
		app := b.ObjectCode
		if replacement {
			app = i.ReplacementScope.Application.AppCode
		}
		want["appCode"] = app
		if key, ok := s["workloadKey"]; ok {
			if !textOK(key) {
				return false
			}
			want["workloadKey"] = key
		}
		if tool == "deployment_restart" {
			if !replacement || s["workloadKey"] == "" {
				return false
			}
			want["requestId"] = b.RequestID
			mutation = true
		}
	case "proxy_observation_get", "proxy_pgcat_metrics_get", "db_proxy_scale", "db_proxy_node_protection_set", "db_proxy_nodes_deregister":
		if !replacement {
			return false
		}
		want["requestId"] = b.RequestID
		want["proxyCode"] = b.ObjectCode
		mutation = tool != "proxy_observation_get" && tool != "proxy_pgcat_metrics_get"
	default:
		return false
	}
	if mutation && b.ExecutionMode == "qualification" {
		return false
	}
	return reflect.DeepEqual(want, s)
}
