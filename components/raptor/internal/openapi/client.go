package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct{ BaseURL, Username, Password string }

func (c Client) Call(ctx context.Context, method, path string, body any) (map[string]any, error) {
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
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
