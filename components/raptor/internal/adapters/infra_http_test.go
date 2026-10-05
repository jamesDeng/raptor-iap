package adapters

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInfraHTTPContract(t *testing.T) {
	status := 200
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("envCode") != "dev" || r.URL.Query().Get("code") != "app" || r.Header.Get("X-Infra-Authorization") == "" {
			t.Error("request contract")
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"data":[]}`))
	}))
	defer s.Close()
	p := HTTPInfra{Username: "u", Password: "p", AuthHeader: "X-Infra-Authorization"}
	env := domain.Environment{Code: "dev", Config: map[string]any{"infraApiUrl": s.URL, "clusterId": "cluster"}}
	o := domain.Object{Kind: "application", Code: "app"}
	rows, e := p.ListDeployments(context.Background(), env, o)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	status = 503
	if _, e = p.ListDeployments(context.Background(), env, o); e == nil {
		t.Fatal("provider failure became empty")
	}
	p.Password = ""
	if _, e = p.ListDeployments(context.Background(), env, o); e == nil {
		t.Fatal("missing secret accepted")
	}
}
func TestInfraNoRedirectCredentialLeak(t *testing.T) {
	calls := 0
	dst := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer dst.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dst.URL, 302) }))
	defer src.Close()
	p := HTTPInfra{Username: "u", Password: "p"}
	for _, u := range []string{src.URL, "http://example.com", "https://u:p@example.com"} {
		_, e := p.ListDeployments(context.Background(), domain.Environment{Code: "dev", Config: map[string]any{"infraApiUrl": u}}, domain.Object{Kind: "application", Code: "app"})
		if e == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	if calls != 0 {
		t.Fatal("credential redirect")
	}
}
func TestInfraScopeResponse(t *testing.T) {
	for _, body := range []string{`{"data":[{"envCode":"other","objectCode":"app","kind":"application","clusterId":"cluster","evidenceMode":"live"}]}`, `{"data":null}`, `{"error":{"code":"bad"}}`, `{"data":[]} trailing`, `{"data":[{"envCode":"dev","objectCode":"app","kind":"application","clusterId":"other","evidenceMode":"live"}]}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		p := HTTPInfra{Username: "u", Password: "p"}
		_, e := p.ListDeployments(context.Background(), domain.Environment{Code: "dev", Config: map[string]any{"infraApiUrl": s.URL, "clusterId": "cluster"}}, domain.Object{Kind: "application", Code: "app"})
		s.Close()
		if e == nil {
			t.Fatal(body)
		}
	}
}
func TestInfraDeploymentStatus(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uid") != "uid" && r.URL.Query().Get("uid") != "wrong" {
			t.Error("identity query")
		}
		w.Write([]byte(`{"data":{"uid":"uid","generation":2,"observedGeneration":2,"replicas":2,"updatedReplicas":2,"readyReplicas":1,"evidenceMode":"live","pods":[]}}`))
	}))
	defer s.Close()
	env := domain.Environment{Code: "dev", Config: map[string]any{"infraApiUrl": s.URL, "clusterId": "cluster"}}
	p := HTTPInfra{Username: "u", Password: "p", ResolveEnvironment: func(context.Context, string) (domain.Environment, error) { return env, nil }}
	target := domain.RestartTarget{EnvCode: "dev", AppCode: "app", ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "uid"}
	st, e := p.GetDeploymentStatus(context.Background(), target)
	if e != nil || st.ReadyReplicas != 1 {
		t.Fatal(st, e)
	}
	target.UID = "wrong"
	if _, e = p.GetDeploymentStatus(context.Background(), target); e == nil {
		t.Fatal("changed uid accepted")
	}
}
