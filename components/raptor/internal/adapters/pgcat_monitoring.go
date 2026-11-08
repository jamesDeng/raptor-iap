package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ReconcilePgcatMonitoring calls the fixed service-only Infra API operation.
// The provider verifies the current catalog, ESS, NLB and ConfigMap identities.
func (p HTTPInfra) ReconcilePgcatMonitoring(ctx context.Context) error {
	if p.ResolveEnvironment == nil || p.Username == "" || p.Password == "" || strings.Contains(p.Username, ":") {
		return domain.ErrUnavailable
	}
	header := p.AuthHeader
	if header == "" {
		header = "Authorization"
	}
	if header != "Authorization" && header != "X-Infra-Authorization" {
		return domain.ErrUnavailable
	}
	env, err := p.ResolveEnvironment(ctx, "rdev.ali")
	if err != nil || env.Code != "rdev.ali" {
		return domain.ErrUnavailable
	}
	raw, _ := env.Config["infraApiUrl"].(string)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return domain.ErrUnavailable
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return domain.ErrUnavailable
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/pgcat-monitoring-reconcile"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return domain.ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(header, "Basic "+base64.StdEncoding.EncodeToString([]byte(p.Username+":"+p.Password)))
	client := http.Client{Timeout: 30 * time.Second}
	if p.Client != nil {
		client = *p.Client
	}
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return domain.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(body) > 4096 {
		return domain.ErrUnavailable
	}
	var envelope struct {
		Data *struct {
			Changed bool `json:"changed"`
			Count   int  `json:"count"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil || envelope.Data.Count < 1 {
		return domain.ErrUnavailable
	}
	return nil
}
