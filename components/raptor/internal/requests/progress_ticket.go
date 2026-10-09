package requests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/transportpolicy"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errProgressDisabled = errors.New("ProgressNotConfigured")

type progressIssuer interface {
	ProgressTicket(context.Context, string, string, string) (map[string]any, error)
}

func (g HTTPGateway) ProgressTicket(ctx context.Context, id, subject, origin string) (map[string]any, error) {
	if !transportpolicy.Allowed(g.BaseURL, "http://agent-gateway:8874", g.AllowClusterHTTP) || g.Username == "" || g.Password == "" {
		return nil, domain.ErrUnavailable
	}
	body, _ := json.Marshal(map[string]string{"subject": subject, "origin": origin})
	r, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(g.BaseURL, "/")+"/v1/requests/"+url.PathEscape(id)+"/progress-ticket", bytes.NewReader(body))
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	r.SetBasicAuth(g.Username, g.Password)
	r.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := c.Do(r)
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 501 {
		return nil, errProgressDisabled
	}
	if resp.StatusCode != 200 {
		return nil, domain.ErrUnavailable
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&out) != nil {
		return nil, domain.ErrUnavailable
	}
	return out.Data, nil
}
func (s *Service) registerProgressTicket(m *http.ServeMux, a *auth.Service) {
	m.Handle("POST /api/v1/requests/{id}/progress-ticket", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Same authenticated request-read policy as View; the POC permits all signed-in users to read requests.
		request, e := s.Get(r.Context(), r.PathValue("id"))
		if e != nil {
			httpx.Result(w, 200, nil, e)
			return
		}
		if request.Definition.Type != "agent" {
			httpx.Error(w, 501, "ProgressNotConfigured")
			return
		}
		origin := r.Header.Get("Origin")
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			httpx.Error(w, 403, "Forbidden")
			return
		}
		issuer, ok := s.Gateway.(progressIssuer)
		if !ok {
			httpx.Error(w, 501, "ProgressNotConfigured")
			return
		}
		v, e := issuer.ProgressTicket(r.Context(), r.PathValue("id"), auth.UserFrom(r).ID, origin)
		if errors.Is(e, errProgressDisabled) {
			httpx.Error(w, 501, "ProgressNotConfigured")
			return
		}
		httpx.Result(w, 200, v, e)
	})))
}
