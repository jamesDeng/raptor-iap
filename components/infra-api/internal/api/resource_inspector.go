package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/jsoninput"
	"raptor-iap/infra-api/internal/resources"
	"reflect"
	"strings"
	"time"
)

// NewResourceAgentInspector resolves complete fresh proof inside the current
// Infra invocation. Preparation is never returned as operation authorization.
func NewResourceAgentInspector(origin, user, password string, client *http.Client, resolve func(context.Context, string, string) (resources.Proof, error)) (AgentCheck, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || user == "" || strings.Contains(user, ":") || password == "" || resolve == nil {
		return nil, domain.ErrNotConfigured
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	call := func(ctx context.Context, path string, input, out any) error {
		raw, e := json.Marshal(input)
		if e != nil || len(raw) > 8192 {
			return domain.ErrScope
		}
		req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(origin, "/")+path, bytes.NewReader(raw))
		if e != nil {
			return domain.ErrScope
		}
		req.SetBasicAuth(user, password)
		req.Header.Set("Content-Type", "application/json")
		resp, e := c.Do(req)
		if e != nil {
			return domain.ErrScope
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return domain.ErrScope
		}
		raw, e = io.ReadAll(io.LimitReader(resp.Body, 8193))
		if e != nil || jsoninput.Decode(raw, out) != nil {
			return domain.ErrScope
		}
		return nil
	}
	return func(ctx context.Context, credential, tool string, selectors map[string]string) (AgentIdentity, error) {
		bad := AgentIdentity{}
		if !agentToken.MatchString(credential) {
			return bad, domain.ErrScope
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		operation := struct {
			Credential string            `json:"credential"`
			Audience   string            `json:"audience"`
			Tool       string            `json:"tool"`
			Selectors  map[string]string `json:"selectors"`
		}{credential, "infra", tool, selectors}
		var prepared struct {
			Data struct {
				Eligible bool          `json:"eligible"`
				Prepared string        `json:"prepared"`
				Identity AgentIdentity `json:"identity"`
			} `json:"data"`
		}
		if e := call(ctx, "/v1/agent-access/prepare-infra", operation, &prepared); e != nil {
			return bad, e
		}
		i := prepared.Data.Identity
		if !prepared.Data.Eligible || len(prepared.Data.Prepared) != 64 || !i.permitsResource(tool, selectors) {
			return bad, domain.ErrScope
		}
		var proof *resources.Proof
		if i.Binding.Operation == "db-proxy.replace-nodes" {
			expected, e := resourceIdentityProof(i)
			if e != nil {
				return bad, e
			}
			fresh, e := resolve(ctx, i.Binding.EnvCode, i.Binding.ObjectCode)
			if e != nil {
				return bad, e
			}
			if fresh != expected {
				return bad, domain.ErrScope
			}
			proof = &fresh
		}
		input := struct {
			Operation any              `json:"operation"`
			Prepared  string           `json:"prepared"`
			Proof     *resources.Proof `json:"proof,omitempty"`
		}{operation, prepared.Data.Prepared, proof}
		var final struct {
			Data AgentIdentity `json:"data"`
		}
		if e := call(ctx, "/v1/agent-access/validate-infra", input, &final); e != nil {
			return bad, e
		}
		if !final.Data.permitsResource(tool, selectors) || !reflect.DeepEqual(final.Data, i) {
			return bad, domain.ErrScope
		}
		return final.Data, nil
	}, nil
}
