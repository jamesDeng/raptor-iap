package provider

import (
	"context"
	"io"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"raptor-iap/infra-api/internal/domain"
	"time"
)

func kubernetesClient(config *rest.Config) (kubernetes.Interface, error) {
	client, e := rest.HTTPClientFor(config)
	if e != nil {
		return nil, e
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return kubernetes.NewForConfigAndClient(config, client)
}

// NewPrometheusQuery reuses the verified private ACK kubeconfig/account path.
// Only this fixed Service is queried; no caller supplies a URL or namespace.
func NewPrometheusQuery() (func(context.Context, domain.Environment, string, string) ([]byte, error), error) {
	r, e := New()
	if e != nil {
		return nil, e
	}
	return r.(*reader).PrometheusQuery, nil
}
func (p *reader) PrometheusQuery(ctx context.Context, env domain.Environment, query, evaluation string) ([]byte, error) {
	unavailable := func() ([]byte, error) { return nil, &domain.CommandError{Code: "MetricsUnavailable"} }
	if ctx.Err() != nil || env.Region != "ap-southeast-1" || env.Code == "" || env.AccountID == "" || env.ClusterID == "" {
		return unavailable()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, e := p.cluster(ctx, env)
	if e != nil || c.CoreV1().RESTClient() == nil {
		return unavailable()
	}
	stream, e := c.CoreV1().RESTClient().Get().AbsPath("/api/v1/namespaces/raptor-test/services/http:rdev-test-metrics-metrics:http/proxy/api/v1/query").Param("query", query).Param("time", evaluation).Stream(ctx)
	if e != nil {
		return unavailable()
	}
	defer stream.Close()
	raw, e := io.ReadAll(io.LimitReader(stream, 1048577))
	if e != nil || len(raw) > 1048576 {
		return unavailable()
	}
	return raw, nil
}
