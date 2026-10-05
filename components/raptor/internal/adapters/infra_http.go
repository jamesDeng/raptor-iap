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

type HTTPInfra struct {
	Username, Password, AuthHeader string
	Client                         *http.Client
	ResolveEnvironment             func(context.Context, string) (domain.Environment, error)
}

func (p HTTPInfra) read(ctx context.Context, env domain.Environment, path string, q url.Values, out any) error {
	if p.Username == "" || p.Password == "" || strings.Contains(p.Username, ":") {
		return domain.ErrUnavailable
	}
	header := p.AuthHeader
	if header == "" {
		header = "Authorization"
	}
	if header != "Authorization" && header != "X-Infra-Authorization" {
		return domain.ErrUnavailable
	}
	raw, _ := env.Config["infraApiUrl"].(string)
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return domain.ErrUnavailable
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return domain.ErrUnavailable
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = q.Encode()
	request, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return domain.ErrUnavailable
	}
	request.Header.Set(header, "Basic "+base64.StdEncoding.EncodeToString([]byte(p.Username+":"+p.Password)))
	client := http.Client{Timeout: 30 * time.Second}
	if p.Client != nil {
		client = *p.Client
	}
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, e := client.Do(request)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return domain.ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
	if e != nil || len(b) > 2*1024*1024 {
		return domain.ErrUnavailable
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(b, &envelope) != nil || len(envelope) != 1 || envelope["data"] == nil || string(envelope["data"]) == "null" {
		return domain.ErrUnavailable
	}
	if json.Unmarshal(envelope["data"], out) != nil {
		return domain.ErrUnavailable
	}
	return nil
}
func (p HTTPInfra) ListDeployments(ctx context.Context, env domain.Environment, o domain.Object) ([]Deployment, error) {
	var rows []Deployment
	e := p.read(ctx, env, "/v1/deployments", url.Values{"envCode": {env.Code}, "kind": {o.Kind}, "code": {o.Code}}, &rows)
	if e != nil {
		return nil, e
	}
	cluster, _ := env.Config["clusterId"].(string)
	for _, r := range rows {
		if r.EnvCode != env.Code || r.ObjectCode != o.Code || r.Kind != o.Kind || (r.EvidenceMode != "live" && r.EvidenceMode != "simulated") || (o.Kind == "application" && (cluster == "" || r.ClusterID != cluster)) {
			return nil, domain.ErrUnavailable
		}
	}
	return rows, nil
}
func (p HTTPInfra) GetDeploymentStatus(ctx context.Context, t domain.RestartTarget) (DeploymentStatus, error) {
	if p.ResolveEnvironment == nil {
		return DeploymentStatus{}, domain.ErrUnavailable
	}
	env, e := p.ResolveEnvironment(ctx, t.EnvCode)
	if e != nil || env.Code != t.EnvCode {
		return DeploymentStatus{}, domain.ErrUnavailable
	}
	cluster, _ := env.Config["clusterId"].(string)
	if cluster == "" || cluster != t.ClusterID {
		return DeploymentStatus{}, domain.ErrUnavailable
	}
	var st DeploymentStatus
	e = p.read(ctx, env, "/v1/deployment-status", url.Values{"envCode": {t.EnvCode}, "appCode": {t.AppCode}, "clusterId": {t.ClusterID}, "namespace": {t.Namespace}, "name": {t.Name}, "uid": {t.UID}}, &st)
	if e != nil {
		return st, e
	}
	if st.UID != t.UID || (st.EvidenceMode != "live" && st.EvidenceMode != "simulated") {
		return DeploymentStatus{}, domain.ErrUnavailable
	}
	return st, nil
}
