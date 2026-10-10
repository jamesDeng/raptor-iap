package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

type ModelLease struct {
	Credential json.RawMessage `json:"credential"`
	Generation int64           `json:"generation"`
	ExpiresAt  time.Time       `json:"expiresAt"`
}
type ModelCredentialClient interface {
	Lease(context.Context, execution.AttemptBinding) (ModelLease, error)
	Refresh(context.Context, execution.AttemptBinding, int64, json.RawMessage) (int64, error)
	Valid(context.Context, execution.AttemptBinding) error
}
type HTTPModelCredentials struct{ BaseURL, Username, Password string }

func (c HTTPModelCredentials) call(ctx context.Context, route string, payload any, out any) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) || c.Username == "" || c.Password == "" {
		return ErrConfiguration
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ErrConfiguration
	}
	r, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.BaseURL, "/")+route, bytes.NewReader(body))
	if err != nil {
		return ErrConfiguration
	}
	r.SetBasicAuth(c.Username, c.Password)
	r.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(r)
	if err != nil {
		return ErrRuntimeUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ErrRuntimeUnavailable
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 65537)).Decode(&envelope) != nil || len(envelope.Data) > 65536 {
		return ErrRuntimeUnavailable
	}
	if json.Unmarshal(envelope.Data, out) != nil {
		return ErrRuntimeUnavailable
	}
	return nil
}
func (c HTTPModelCredentials) Lease(ctx context.Context, b execution.AttemptBinding) (ModelLease, error) {
	var out ModelLease
	err := c.call(ctx, "/internal/v1/model-credentials/lease", map[string]any{"requestId": b.RequestID, "attemptId": b.AttemptID, "providerId": b.ProviderID, "modelId": b.Model, "connectionVersion": b.ConnectionVersion}, &out)
	if err != nil || out.Generation < 1 || len(out.Credential) == 0 || out.ExpiresAt.Before(time.Now().Add(4*time.Minute)) {
		return ModelLease{}, ErrRuntimeUnavailable
	}
	return out, nil
}
func (c HTTPModelCredentials) Refresh(ctx context.Context, b execution.AttemptBinding, generation int64, credential json.RawMessage) (int64, error) {
	var out struct {
		Generation int64 `json:"generation"`
	}
	err := c.call(ctx, "/internal/v1/model-credentials/refresh", map[string]any{"binding": map[string]any{"requestId": b.RequestID, "attemptId": b.AttemptID, "providerId": b.ProviderID, "modelId": b.Model, "connectionVersion": b.ConnectionVersion}, "expectedGeneration": generation, "credential": credential}, &out)
	if err != nil || out.Generation <= generation {
		return 0, ErrRuntimeUnavailable
	}
	return out.Generation, nil
}
func (c HTTPModelCredentials) Valid(ctx context.Context, b execution.AttemptBinding) error {
	var out struct {
		Valid bool `json:"valid"`
	}
	err := c.call(ctx, "/internal/v1/model-credentials/validate", map[string]any{"requestId": b.RequestID, "attemptId": b.AttemptID, "providerId": b.ProviderID, "modelId": b.Model, "connectionVersion": b.ConnectionVersion}, &out)
	if err != nil || !out.Valid {
		return ErrRuntimeUnavailable
	}
	return nil
}
