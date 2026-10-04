package execution

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPRaptor struct{ BaseURL, Username, Password string }

func (c HTTPRaptor) Context(ctx context.Context, id string) (ExecutionInput, error) {
	var out ExecutionInput
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return out, ErrInvalid
	}
	if c.Username == "" || c.Password == "" {
		return out, ErrUnavailable
	}
	r, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.BaseURL, "/")+"/v1/requests/"+url.PathEscape(id)+"/context", nil)
	if e != nil {
		return out, ErrInvalid
	}
	r.SetBasicAuth(c.Username, c.Password)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r)
	if e != nil {
		return out, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, ErrUnavailable
	}
	var envelope struct {
		Data ExecutionInput `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&envelope) != nil || envelope.Data.RequestID != id {
		return out, ErrUnavailable
	}
	return envelope.Data, nil
}
