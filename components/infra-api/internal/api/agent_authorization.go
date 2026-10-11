package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type AgentBinding struct {
	RequestID  string `json:"requestId"`
	AttemptID  string `json:"attemptId"`
	Operation  string `json:"operation"`
	ObjectKind string `json:"objectKind"`
	ObjectCode string `json:"objectCode"`
	EnvCode    string `json:"envCode"`
	ClusterID  string `json:"clusterId"`
}
type AgentApplication struct {
	EnvCode   string `json:"envCode"`
	AppCode   string `json:"appCode"`
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}
type AgentReplacementScope struct {
	EnvCode       string           `json:"envCode"`
	ProxyCode     string           `json:"proxyCode"`
	GroupID       string           `json:"groupId"`
	ServerGroupID string           `json:"serverGroupId"`
	TargetDBCode  string           `json:"targetDbCode"`
	Application   AgentApplication `json:"application"`
}
type AgentIdentity struct {
	Active           bool                   `json:"active"`
	Binding          *AgentBinding          `json:"binding"`
	ExpiresAt        *time.Time             `json:"expiresAt"`
	ReplacementScope *AgentReplacementScope `json:"replacementScope,omitempty"`
}
type AgentCheck func(context.Context, string, string, map[string]string) (AgentIdentity, error)

var agentToken = regexp.MustCompile(`^raptor_at_[A-Za-z0-9_-]{43}$`)

func (i AgentIdentity) permits(tool string, selectors map[string]string) bool {
	b := i.Binding
	if !i.Active || b == nil || i.ExpiresAt == nil || !i.ExpiresAt.After(time.Now()) || !textOK(b.RequestID) || !textOK(b.AttemptID) || !textOK(b.EnvCode) || !textOK(b.ObjectCode) || !textOK(b.ClusterID) {
		return false
	}
	replacement := b.Operation == "db-proxy.replace-nodes" && b.ObjectKind == "db-proxy"
	question := b.Operation == "application.question" && b.ObjectKind == "application"
	if !replacement && !question {
		return false
	}
	s := i.ReplacementScope
	if replacement {
		if s == nil || s.EnvCode != b.EnvCode || s.ProxyCode != b.ObjectCode || s.Application.EnvCode != b.EnvCode || s.Application.ClusterID != b.ClusterID {
			return false
		}
		for _, v := range []string{s.GroupID, s.ServerGroupID, s.TargetDBCode, s.Application.AppCode, s.Application.Namespace, s.Application.Name, s.Application.UID} {
			if !textOK(v) {
				return false
			}
		}
	}
	if tool == "" {
		return len(selectors) == 0
	}
	want := map[string]string{"envCode": b.EnvCode}
	switch tool {
	case "cloud_identity_get":
	case "deployments_list":
		want["kind"] = b.ObjectKind
		want["code"] = b.ObjectCode
	case "deployment_status_get", "deployment_restart":
		if tool == "deployment_restart" && !replacement {
			return false
		}
		if replacement {
			a := s.Application
			want["appCode"] = a.AppCode
			want["clusterId"] = a.ClusterID
			want["namespace"] = a.Namespace
			want["name"] = a.Name
			want["uid"] = a.UID
		} else {
			want["appCode"] = b.ObjectCode
			want["clusterId"] = b.ClusterID
			for _, k := range []string{"namespace", "name", "uid"} {
				if !textOK(selectors[k]) {
					return false
				}
				want[k] = selectors[k]
			}
		}
		if tool == "deployment_restart" {
			want["requestId"] = b.RequestID
		}
	case "db_proxy_scale", "db_proxy_node_protection_set", "db_proxy_nodes_deregister":
		if !replacement {
			return false
		}
		want["requestId"] = b.RequestID
		want["proxyCode"] = s.ProxyCode
		want["groupId"] = s.GroupID
		if tool == "db_proxy_nodes_deregister" {
			want["serverGroupId"] = s.ServerGroupID
		}
	default:
		return false
	}
	if len(want) != len(selectors) {
		return false
	}
	for k, v := range want {
		if selectors[k] != v || !textOK(v) {
			return false
		}
	}
	return true
}

type agentAuthorizer struct {
	credential string
	check      AgentCheck
	base       Authorizer
	identity   AgentIdentity
}

func (a agentAuthorizer) VisibleCommand(path string) bool {
	if a.identity.Binding == nil || a.identity.Binding.Operation != "db-proxy.replace-nodes" {
		return false
	}
	if f, ok := a.base.(interface{ VisibleCommand(string) bool }); ok {
		return f.VisibleCommand(path)
	}
	return a.base != nil
}
func agentSelectors(path string, args any) (string, map[string]string, error) {
	tool := ""
	selectors := map[string]string{}
	switch path {
	case "/v1/cloud/identity":
		tool = "cloud_identity_get"
	case "/v1/deployments":
		tool = "deployments_list"
	case "/v1/deployment-status":
		tool = "deployment_status_get"
	default:
		for _, s := range commandSpecs {
			if s.path == path {
				tool = s.name
			}
		}
	}
	if tool == "" {
		return "", nil, domain.ErrScope
	}
	if q, ok := args.(url.Values); ok {
		for k, v := range q {
			if len(v) != 1 {
				return "", nil, domain.ErrScope
			}
			selectors[k] = v[0]
		}
		return tool, selectors, nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", nil, domain.ErrScope
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return "", nil, domain.ErrScope
	}
	keys := []string{"requestId", "envCode", "proxyCode", "groupId"}
	if tool == "db_proxy_nodes_deregister" {
		keys = append(keys, "serverGroupId")
	}
	if tool == "deployment_restart" {
		keys = []string{"requestId", "envCode", "appCode", "clusterId", "namespace", "name", "uid"}
	}
	for _, k := range keys {
		var v string
		if json.Unmarshal(fields[k], &v) != nil || !textOK(v) {
			return "", nil, domain.ErrScope
		}
		selectors[k] = v
	}
	return tool, selectors, nil
}
func (a agentAuthorizer) Authorize(ctx context.Context, _ string, path string, args any) error {
	tool, selectors, err := agentSelectors(path, args)
	if strings.HasPrefix(tool, "db_proxy_") || tool == "deployment_restart" {
		if !a.VisibleCommand(path) {
			return domain.ErrScope
		}
	}
	if err != nil {
		return domain.ErrScope
	}
	identity, err := a.check(ctx, a.credential, tool, selectors)
	if err != nil || !identity.permits(tool, selectors) || a.identity.Binding == nil || identity.Binding == nil || !reflect.DeepEqual(identity.Binding, a.identity.Binding) || !reflect.DeepEqual(identity.ReplacementScope, a.identity.ReplacementScope) {
		return domain.ErrScope
	}
	return nil
}

// NewAgentInspector uses private service credentials only on the controller-side
// introspection hop. No credential or positive authorization cache is returned.
func NewAgentInspector(origin, user, password string, client *http.Client) (AgentCheck, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || user == "" || password == "" {
		return nil, domain.ErrNotConfigured
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return func(ctx context.Context, credential, tool string, selectors map[string]string) (AgentIdentity, error) {
		bad := AgentIdentity{}
		if !agentToken.MatchString(credential) {
			return bad, domain.ErrScope
		}
		raw, _ := json.Marshal(map[string]any{"credential": credential, "audience": "infra", "tool": tool, "selectors": selectors})
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(origin, "/")+"/v1/agent-access/introspect", bytes.NewReader(raw))
		if err != nil {
			return bad, domain.ErrScope
		}
		req.SetBasicAuth(user, password)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return bad, domain.ErrScope
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return bad, domain.ErrScope
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
		if err != nil || len(body) > 16384 {
			return bad, domain.ErrScope
		}
		var envelope struct {
			Data AgentIdentity `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil || !envelope.Data.permits(tool, selectors) {
			return bad, errors.New("agent access denied")
		}
		return envelope.Data, nil
	}, nil
}
