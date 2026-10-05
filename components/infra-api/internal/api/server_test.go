package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

type fakeReader struct {
	account string
	err     error
}

func (f fakeReader) Identity(context.Context, domain.Environment) (string, error) {
	return f.account, f.err
}
func (f fakeReader) Deployments(context.Context, domain.Environment, string, string) ([]domain.Deployment, error) {
	return nil, f.err
}
func (f fakeReader) Status(context.Context, domain.Environment, domain.Target) (domain.Status, error) {
	return domain.Status{}, f.err
}
func call(t *testing.T, f fakeReader, path string, auth []string) *httptest.ResponseRecorder {
	t.Helper()
	h, e := New(f, map[string]domain.Environment{"dev": {Code: "dev", AccountID: "123", Region: "ap-southeast-1"}}, "user", "secret", "X-Infra-Authorization")
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", path, nil)
	for _, v := range auth {
		r.Header.Add("X-Infra-Authorization", v)
	}
	r.Header.Set("Authorization", valid())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func valid() string { return "Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret")) }
func TestHTTPAuthAndSelectors(t *testing.T) {
	for _, a := range [][]string{nil, {"Basic bad"}, {valid(), valid()}, {"Bearer secret"}, {"Basic " + base64.StdEncoding.EncodeToString([]byte("user:wrong"))}} {
		if w := call(t, fakeReader{}, "/v1/cloud/identity?envCode=dev", a); w.Code != 401 {
			t.Fatalf("auth %d", w.Code)
		}
	}
	w := call(t, fakeReader{}, "/healthz", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "account") {
		t.Fatal(w.Body.String())
	}
}
func TestScopeAndIdentity(t *testing.T) {
	for _, x := range []struct {
		env, account string
		want         int
	}{{"unknown", "123", 403}, {"dev", "456", 403}, {"dev", "123", 200}} {
		w := call(t, fakeReader{account: x.account}, "/v1/cloud/identity?envCode="+x.env, []string{valid()})
		if w.Code != x.want {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "123") || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("secret projection")
		}
	}
}
func TestDiscoveryErrors(t *testing.T) {
	p := "/v1/deployments?envCode=dev&kind=database&code=db"
	w := call(t, fakeReader{err: errors.New("private")}, p, []string{valid()})
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Body.String())
	}
	w = call(t, fakeReader{}, p, []string{valid()})
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "{\"data\":[]}" {
		t.Fatal(w.Body.String())
	}
	for _, s := range []string{"&envCode=dev", "&unknown=x", "&code=" + strings.Repeat("x", 257)} {
		if w := call(t, fakeReader{}, p+s, []string{valid()}); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
