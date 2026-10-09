package requests

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/transportpolicy"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HTTPGateway struct {
	BaseURL, Username, Password string
	Client                      *http.Client
	AllowClusterHTTP            bool
}

func (g HTTPGateway) read(ctx context.Context, path string, out any) error {
	if !transportpolicy.Allowed(g.BaseURL, "http://agent-gateway:8874", g.AllowClusterHTTP) || g.Username == "" || g.Password == "" {
		return domain.ErrUnavailable
	}

	r, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(g.BaseURL, "/")+path, nil)
	if e != nil {
		return domain.ErrUnavailable
	}
	r.SetBasicAuth(g.Username, g.Password)
	c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := c.Do(r)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return domain.ErrUnavailable
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out) != nil {
		return domain.ErrUnavailable
	}
	return nil
}
func (g HTTPGateway) Execution(ctx context.Context, id string) (map[string]any, error) {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	e := g.read(ctx, "/v1/requests/"+url.PathEscape(id), &envelope)
	return envelope.Data, e
}
func (g HTTPGateway) Progress(ctx context.Context, id string, after int64) ([]GatewayEvent, error) {
	var envelope struct {
		Data []GatewayEvent `json:"data"`
	}
	e := g.read(ctx, "/v1/requests/"+url.PathEscape(id)+"/events?after="+strconv.FormatInt(after, 10), &envelope)
	return envelope.Data, e
}

func (g HTTPGateway) send(ctx context.Context, method, path string, body any) error {
	if !transportpolicy.Allowed(g.BaseURL, "http://agent-gateway:8874", g.AllowClusterHTTP) {
		return domain.ErrInvalid
	}

	if g.Username == "" || g.Password == "" {
		return domain.ErrUnavailable
	}
	b, e := json.Marshal(body)
	if e != nil {
		return domain.ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(g.BaseURL, "/")+path, bytes.NewReader(b))
	if e != nil {
		return domain.ErrInvalid
	}
	req.SetBasicAuth(g.Username, g.Password)
	req.Header.Set("Content-Type", "application/json")
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, e := client.Do(req)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 400 || resp.StatusCode == 404 || resp.StatusCode == 422 {
		return domain.ErrInvalid
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.ErrUnavailable
	}
	return nil
}
func (g HTTPGateway) PutRequest(ctx context.Context, id string) error {
	return g.send(ctx, "PUT", "/v1/requests/"+url.PathEscape(id), map[string]string{"requestId": id, "type": "agent"})
}
func (g HTTPGateway) SendSignal(ctx context.Context, id string, payload json.RawMessage) error {
	return g.send(ctx, "POST", "/v1/requests/"+url.PathEscape(id)+"/signals", payload)
}

func (g HTTPGateway) Conversation(ctx context.Context, id string) (ConversationCapability, error) {
	var out struct {
		Data ConversationCapability `json:"data"`
	}
	e := g.read(ctx, "/v1/requests/"+url.PathEscape(id)+"/conversation", &out)
	return out.Data, e
}
func (g HTTPGateway) PutMessage(ctx context.Context, id string, payload json.RawMessage) (MessageReceipt, error) {
	var out struct {
		Data MessageReceipt `json:"data"`
	}
	if !transportpolicy.Allowed(g.BaseURL, "http://agent-gateway:8874", g.AllowClusterHTTP) || g.Username == "" || g.Password == "" {
		return out.Data, domain.ErrUnavailable
	}
	r, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(g.BaseURL, "/")+"/v1/requests/"+url.PathEscape(id)+"/messages", bytes.NewReader(payload))
	if e != nil {
		return out.Data, domain.ErrInvalid
	}
	r.SetBasicAuth(g.Username, g.Password)
	r.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := c.Do(r)
	if e != nil {
		return out.Data, domain.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out.Data, domain.ErrUnavailable
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&out) != nil {
		return out.Data, domain.ErrUnavailable
	}
	return out.Data, nil
}
