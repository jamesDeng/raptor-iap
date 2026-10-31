package main

import (
	"context"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/resources"
	"strings"
	"testing"
)

type startupResourceResolver struct{ calls int }

func (r *startupResourceResolver) Resolve(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	r.calls++
	return domain.ResolvedResource{Context: domain.ResourceContext{EnvCode: env, Kind: kind, Code: code}, Environment: domain.Environment{Code: env}}, nil
}
func (r *startupResourceResolver) ResolveProof(context.Context, string, string) (resources.Proof, error) {
	return resources.Proof{}, domain.ErrScope
}
func TestResourceStartupUsesLogicalServerWithoutStaticScope(t *testing.T) {
	resolver := &startupResourceResolver{}
	h, e := buildResourceRuntimeHandler(&startupReader{}, resolver, "service", "fixture", "Authorization", runtimeOptions{}, runtimeDependencies{})
	if e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("GET", "/v1/deployments?envCode=dev&kind=application&code=app", nil)
	request.SetBasicAuth("service", "fixture")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != 200 || resolver.calls != 1 {
		t.Fatal(w.Code, w.Body.String(), resolver.calls)
	}
	request = httptest.NewRequest("GET", "/v1/cloud/identity?envCode=dev", nil)
	request.SetBasicAuth("service", "fixture")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code == 200 {
		t.Fatal("legacy provider route active")
	}
}

type startupProxyResolver struct{ startupResourceResolver }

func (r *startupProxyResolver) Resolve(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	r.calls++
	environment := proxyScopes()[env]
	mapping := environment.Proxies[0]
	return domain.ResolvedResource{Context: domain.ResourceContext{EnvCode: env, Kind: kind, Code: code}, Environment: environment, Proxy: &mapping}, nil
}
func TestResourceStartupPreservesRuntimeGuardsWithoutStaticScope(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		want, writes int
	}{
		{"scale-out", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"proxy","desiredCapacity":3}`, 200, 1},
		{"scale-in-without-approval", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"proxy","desiredCapacity":1}`, 409, 0},
		{"retired-provider-contract", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"proxy","groupId":"asg","desiredCapacity":3}`, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &proxyDependencies{}
			resolver := &startupProxyResolver{}
			h, e := buildResourceRuntimeHandler(&startupReader{}, resolver, "service", "fixture", "Authorization", runtimeOptions{Proxy: true}, runtimeDependencies{Backend: f, Metrics: f, Approval: f, Claims: f, Fleet: f})
			if e != nil {
				t.Fatal(e)
			}
			r := httptest.NewRequest("POST", "/v1/db-proxy-scale", strings.NewReader(tc.body))
			r.SetBasicAuth("service", "fixture")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || f.calls != tc.writes {
				t.Fatal(w.Code, w.Body.String(), f.calls)
			}
			if tc.want == 400 && resolver.calls != 0 {
				t.Fatal("provider input reached resolver")
			}
		})
	}
}
