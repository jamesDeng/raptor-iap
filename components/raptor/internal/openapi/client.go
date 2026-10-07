package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/transportpolicy"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL, Username, Password string
	AllowClusterHTTP            bool
}

func (c Client) Call(ctx context.Context, method, path string, body any) (map[string]any, error) {
	if !transportpolicy.Allowed(c.BaseURL, "http://raptor-backend:8871", c.AllowClusterHTTP) {
		return nil, errors.New("invalid backend")
	}

	b, e := json.Marshal(body)
	if e != nil {
		return nil, errors.New("InvalidInput")
	}
	r, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(b))
	if e != nil {
		return nil, errors.New("InvalidInput")
	}
	r.Header.Set("Content-Type", "application/json")
	r.SetBasicAuth(c.Username, c.Password)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r)
	if e != nil {
		return nil, errors.New("Unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("backend rejected request")
	}
	var v map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&v) != nil {
		return nil, errors.New("Unavailable")
	}
	return v, nil
}
