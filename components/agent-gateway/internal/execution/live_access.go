package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c HTTPRaptor) Issue(ctx context.Context, b AttemptBinding, hash string, expires time.Time) (AgentAccess, error) {
	var out AgentAccess
	payload, _ := json.Marshal(map[string]any{"attemptId": b.AttemptID, "definitionSha256": hash, "expiresAt": expires.UTC()})
	raw, e := c.accessCall(ctx, "POST", "/v1/requests/"+url.PathEscape(b.RequestID)+"/agent-access", payload)
	if e != nil {
		return out, e
	}
	var envelope struct {
		Data struct {
			Credential   string         `json:"credential"`
			ExpiresAt    time.Time      `json:"expiresAt"`
			Binding      AttemptBinding `json:"binding"`
			RaptorMcpURL string         `json:"raptorMcpUrl"`
			InfraMcpURL  string         `json:"infraMcpUrl"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return out, ErrUnavailable
	}
	v := envelope.Data
	if v.Binding != b || len(v.Credential) < 32 || len(v.Credential) > 4096 || v.ExpiresAt.Before(time.Now()) || v.ExpiresAt.After(expires) {
		return out, ErrUnavailable
	}
	return AgentAccess{Credential: v.Credential, ExpiresAt: v.ExpiresAt, Binding: v.Binding, RaptorMcpURL: v.RaptorMcpURL, InfraMcpURL: v.InfraMcpURL}, nil
}
func (c HTTPRaptor) Revoke(ctx context.Context, b AttemptBinding) error {
	_, e := c.accessCall(ctx, "DELETE", "/v1/requests/"+url.PathEscape(b.RequestID)+"/agent-access/"+url.PathEscape(b.AttemptID), nil)
	return e
}
func (c HTTPRaptor) accessCall(ctx context.Context, method, route string, body []byte) ([]byte, error) {
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) || c.Username == "" || c.Password == "" {
		return nil, ErrInvalid
	}
	r, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+route, bytes.NewReader(body))
	if e != nil {
		return nil, ErrInvalid
	}
	r.SetBasicAuth(c.Username, c.Password)
	r.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return nil, ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(raw) > 65536 {
		return nil, ErrUnavailable
	}
	return raw, nil
}
