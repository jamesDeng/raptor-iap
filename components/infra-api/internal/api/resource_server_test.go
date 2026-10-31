package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/resources"
	"strings"
	"testing"
	"time"
)

type resourceFixture struct {
	calls, proofs int
	rows          domain.ResolvedResource
	proof         resources.Proof
}

func (f *resourceFixture) Resolve(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	f.calls++
	r := f.rows
	r.Context.EnvCode = env
	r.Context.Kind = kind
	r.Context.Code = code
	return r, nil
}
func (f *resourceFixture) ResolveProof(context.Context, string, string) (resources.Proof, error) {
	f.proofs++
	return f.proof, nil
}
func resourceRows() domain.ResolvedResource {
	m := domain.ProxyMapping{Code: "p", GroupID: "g", ServerGroupID: "sg", ListenerID: "l", Port: 5432, TargetDBCode: "db"}
	return domain.ResolvedResource{Environment: domain.Environment{Code: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "c", Proxies: []domain.ProxyMapping{m}}, Proxy: &m, PgproxyWorkload: &domain.PgproxyWorkload{Type: "ess", Code: "p", WorkloadKey: "pk", TargetDBCode: "db"}, DatabaseWorkloads: []domain.DatabaseWorkload{{Type: "rds", Code: "db", ResourceID: "rds"}}, ApplicationWorkloads: []domain.ApplicationWorkload{{Type: "kubernetes", WorkloadKey: "wk", ClusterID: "c", Namespace: "n", Name: "app", UID: "uid", Target: domain.Target{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "uid"}}}}
}
func resourceHandler(t *testing.T, f *resourceFixture, check AgentCheck) http.Handler {
	t.Helper()
	h, e := NewResourceServer(&observedReader{}, f, "user", "secret", "X-Infra-Authorization", nil, NewServiceAuthorizer("user"), check)
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func resourceHTTP(h http.Handler, method, path, body, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("X-Infra-Authorization", auth)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func agentCredential() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("agent:raptor_at_"+strings.Repeat("a", 43)))
}
func resourceIdentity() AgentIdentity {
	expires := time.Now().Add(time.Hour)
	return AgentIdentity{Active: true, ExpiresAt: &expires, Binding: &AgentBinding{RequestID: "r", AttemptID: "a", EnvCode: "dev", ObjectKind: "application", ObjectCode: "app", ClusterID: "c", Operation: "application.question"}}
}
func TestResourceHTTPLogicalEnvelopeAndNoCloudRoute(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	h := resourceHandler(t, f, nil)
	w := resourceHTTP(h, "GET", "/v1/deployments?envCode=dev&kind=application&code=app", "", valid())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"applicationWorkloads"`) || !strings.Contains(w.Body.String(), `"resource":{"envCode":"dev","kind":"application","code":"app"}`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = resourceHTTP(h, "GET", "/v1/cloud/identity?envCode=dev", "", valid())
	if w.Code != 404 || f.calls != 1 {
		t.Fatal(w.Code, f.calls)
	}
}
func TestResourceRejectsSelectorsBeforeResolution(t *testing.T) {
	for _, suffix := range []string{"&clusterId=c", "&code=app", "&url=https://evil", "&unknown=x", "&code=x%3Fy%3Dz"} {
		f := &resourceFixture{}
		w := resourceHTTP(resourceHandler(t, f, nil), "GET", "/v1/deployments?envCode=dev&kind=application&code=app"+suffix, "", valid())
		if w.Code != 400 || f.calls != 0 {
			t.Fatal(suffix, w.Code, f.calls)
		}
	}
}
func TestResourceAgentTypedAuthorizationBeforeResolution(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	var tools []string
	check := func(_ context.Context, _ string, tool string, s map[string]string) (AgentIdentity, error) {
		tools = append(tools, tool)
		if tool == "" {
			t.Error("typed call pre-introspected without selectors")
		}
		return resourceIdentity(), nil
	}
	h := resourceHandler(t, f, check)
	w := resourceHTTP(h, "GET", "/v1/deployments?envCode=dev&kind=application&code=foreign", "", agentCredential())
	if w.Code != 403 || f.calls != 0 || len(tools) != 1 || tools[0] != "deployments_list" {
		t.Fatal(w.Code, f.calls, tools)
	}
	w = resourceHTTP(h, "GET", "/v1/deployments?envCode=dev&kind=application&code=app", "", agentCredential())
	if w.Code != 200 || f.calls != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestResourceProofIsServiceOnly(t *testing.T) {
	f := &resourceFixture{}
	checks := 0
	h := resourceHandler(t, f, func(context.Context, string, string, map[string]string) (AgentIdentity, error) {
		checks++
		return resourceIdentity(), nil
	})
	w := resourceHTTP(h, "GET", "/v1/resource-resolution-proof?envCode=dev&proxyCode=p", "", agentCredential())
	if w.Code != 403 || checks != 0 || f.proofs != 0 {
		t.Fatal(w.Code, checks, f.proofs)
	}
	w = resourceHTTP(h, "GET", "/v1/resource-resolution-proof?envCode=dev&proxyCode=p", "", valid())
	if w.Code != 200 || f.proofs != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestResourceApplicationSelectionIsScopedAndAmbiguityFails(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	f.rows.ApplicationWorkloads = append(f.rows.ApplicationWorkloads, domain.ApplicationWorkload{WorkloadKey: "second"})
	h := resourceHandler(t, f, nil)
	for _, suffix := range []string{"", "&workloadKey=foreign"} {
		w := resourceHTTP(h, "GET", "/v1/deployment-status?envCode=dev&appCode=app"+suffix, "", valid())
		if w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := resourceHTTP(h, "GET", "/v1/deployment-status?envCode=dev&appCode=app&workloadKey=wk", "", valid())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestResourceMCPLogicalOnlyInventoryAndStrictArguments(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	h := resourceHandler(t, f, nil)
	rpc := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		r.Header.Set("X-Infra-Authorization", valid())
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "cloud_identity_get") || strings.Contains(w.Body.String(), "resource-resolution-proof") || strings.Contains(w.Body.String(), `"groupId"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, args := range []string{`{"envCode":"dev","kind":"application","code":"app","groupId":"g"}`, `{"envCode":"dev","kind":"application","code":"app","code":"app"}`, `{"envCode":1,"kind":"application","code":"app"}`} {
		w = rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"deployments_list","arguments":` + args + `}}`)
		var v any
		if json.Unmarshal(w.Body.Bytes(), &v) != nil || !strings.Contains(w.Body.String(), "InvalidInput") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if f.calls != 0 {
		t.Fatal(f.calls)
	}
}

func TestResourceReplacementDiscoveryAllowsOnlyExactDependencies(t *testing.T) {
	for _, mode := range []string{"replacement", "qualification"} {
		for _, tc := range []struct {
			kind, code string
			want       int
		}{{"db-proxy", "p", 200}, {"application", "app", 200}, {"database", "db", 200}, {"application", "other", 403}, {"database", "other", 403}, {"db-proxy", "other", 403}} {
			i, _ := replacementResourceIdentity()
			i.Binding.ExecutionMode = mode
			f := &resourceFixture{rows: resourceRows()}
			h := resourceHandler(t, f, func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil })
			w := resourceHTTP(h, "GET", "/v1/deployments?envCode=dev&kind="+tc.kind+"&code="+tc.code, "", agentCredential())
			if w.Code != tc.want || (tc.want == 403 && f.calls != 0) {
				t.Fatalf("%s %s/%s: %d calls=%d", mode, tc.kind, tc.code, w.Code, f.calls)
			}
		}
	}
}

func TestResourceInvalidProxyPayloadNeverIntrospects(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	checks := 0
	h := resourceHandler(t, f, func(context.Context, string, string, map[string]string) (AgentIdentity, error) {
		checks++
		return AgentIdentity{}, nil
	})
	for _, args := range []string{`{"requestId":"r","envCode":"dev","proxyCode":"p","kind":"fleet","groupId":"g"}`, `{"requestId":"r","envCode":"dev","proxyCode":"p","kind":"arbitrary"}`, `{"requestId":"r","envCode":"https://evil","proxyCode":"p","kind":"fleet"}`, `{"requestId":"r","envCode":"dev","proxyCode":"p","kind":null}`} {
		w := resourceRPC(h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"proxy_observation_get","arguments":`+args+`}}`, agentCredential())
		if !strings.Contains(w.Body.String(), "InvalidInput") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if checks != 0 || f.calls != 0 {
		t.Fatal(checks, f.calls)
	}
}

func TestResourceGenericReplacementReadsRejectIncompleteProofBeforeResolver(t *testing.T) {
	for _, route := range []string{"/v1/deployments?envCode=dev&kind=db-proxy&code=p", "/v1/deployment-status?envCode=dev&appCode=app&workloadKey=wk"} {
		for _, mode := range []string{"replacement", "qualification"} {
			for _, missing := range []string{"digest", "account", "listener", "port", "database", "inconsistent-digest"} {
				i, p := replacementResourceIdentity()
				i.Binding.ExecutionMode = mode
				switch missing {
				case "digest":
					i.ReplacementScope.ResolutionDigest = ""
				case "account":
					i.ReplacementScope.AccountID = ""
				case "listener":
					i.ReplacementScope.ListenerID = ""
				case "port":
					i.ReplacementScope.Port = 0
				case "database":
					i.ReplacementScope.DatabaseResourceID = ""
				case "inconsistent-digest":
					i.ReplacementScope.ResolutionDigest = strings.Repeat("0", 64)
				}
				f := &resourceFixture{rows: resourceRows(), proof: p}
				h := resourceHandler(t, f, func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil })
				w := resourceHTTP(h, "GET", route, "", agentCredential())
				if w.Code != 409 || f.calls != 0 || f.proofs != 0 {
					t.Fatalf("%s %s %s: %d calls=%d proofs=%d", route, mode, missing, w.Code, f.calls, f.proofs)
				}
			}
		}
	}
}
func TestResourceGenericReplacementReadsRejectActualIdentityDrift(t *testing.T) {
	for _, kind := range []string{"application", "database", "db-proxy"} {
		i, p := replacementResourceIdentity()
		f := &resourceFixture{rows: resourceRows(), proof: p}
		code := "p"
		switch kind {
		case "application":
			code = "app"
			f.rows.ApplicationWorkloads[0].Target.UID = "other"
		case "database":
			code = "db"
			f.rows.DatabaseWorkloads = []domain.DatabaseWorkload{{Type: "rds", ResourceID: "other"}}
		case "db-proxy":
			f.rows.Proxy.ListenerID = "other"
		}
		h := resourceHandler(t, f, func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil })
		w := resourceHTTP(h, "GET", "/v1/deployments?envCode=dev&kind="+kind+"&code="+code, "", agentCredential())
		if w.Code != 409 || f.proofs != 0 {
			t.Fatal(kind, w.Code, w.Body.String(), f.proofs)
		}
	}
}

func TestResourceProofUsesExistingDeploymentsRouteServiceRepresentation(t *testing.T) {
	for _, mode := range []string{"service", "agent", "no-authorizer", "ordinary", "foreign-kind", "cloud-selector"} {
		t.Run(mode, func(t *testing.T) {
			f := &resourceFixture{rows: resourceRows(), proof: resources.Proof{ResolutionDigest: "private-proof"}}
			checks := 0
			check := func(context.Context, string, string, map[string]string) (AgentIdentity, error) {
				checks++
				return resourceIdentity(), nil
			}
			var authorizer Authorizer = NewServiceAuthorizer("user")
			if mode == "no-authorizer" {
				authorizer = nil
			}
			h, e := NewResourceServer(&observedReader{}, f, "user", "secret", "X-Infra-Authorization", nil, authorizer, check)
			if e != nil {
				t.Fatal(e)
			}
			path := "/v1/deployments?envCode=dev&kind=db-proxy&code=p"
			if mode == "foreign-kind" {
				path = "/v1/deployments?envCode=dev&kind=application&code=app"
			}
			if mode == "cloud-selector" {
				path += "&groupId=g"
			}
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("Accept", "application/vnd.raptor.resolution-proof+json")
			r.Header.Set("X-Infra-Authorization", valid())
			if mode == "agent" {
				r.Header.Set("X-Infra-Authorization", agentCredential())
			}
			if mode == "ordinary" {
				r.Header.Set("Accept", "application/json")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			switch mode {
			case "service":
				if w.Code != 200 || f.proofs != 1 || f.calls != 0 || !strings.Contains(w.Body.String(), "private-proof") {
					t.Fatal(w.Code, w.Body.String(), f.proofs, f.calls)
				}
			case "ordinary":
				if w.Code != 200 || f.proofs != 0 || f.calls != 1 || strings.Contains(w.Body.String(), "private-proof") {
					t.Fatal(w.Code, w.Body.String(), f.proofs, f.calls)
				}
			default:
				if w.Code == 200 || f.proofs != 0 || f.calls != 0 || checks != 0 {
					t.Fatal("proof representation reached unauthorized resolver", w.Code, f.proofs, f.calls, checks)
				}
			}
		})
	}
}
