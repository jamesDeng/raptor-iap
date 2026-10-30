package resources

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const validContext = `{"data":{"envCode":"dev","kind":"db-proxy","code":"proxy","cloud":"aliyun","region":"ap-southeast-1","accountId":"123","clusterId":"ack","relatedResources":[]}}`

func TestCatalogFixedAuthenticatedBoundedRead(t *testing.T) {
	c, e := NewCatalog("https://catalog.internal", "svc", "secret", &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "svc" || p != "secret" || r.URL.String() != "https://catalog.internal/v1/infra/resource-context?code=proxy&envCode=dev&kind=db-proxy" {
			t.Fatalf("untrusted request %v", r.URL)
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Fatal("unbounded catalog read")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(validContext))}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	out, e := c.Context(context.Background(), "dev", "db-proxy", "proxy")
	if e != nil || out.Code != "proxy" || out.AccountID != "123" {
		t.Fatal(out, e)
	}
}
func TestCatalogRejectsForeignMalformedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":null}`, strings.Replace(validContext, `"dev"`, `"foreign"`, 1), strings.Replace(validContext, `"db-proxy"`, `"application"`, 1), strings.Replace(validContext, `"accountId":"123"`, `"accountId":"123","config":{"secret":"bad"}`, 1), validContext + `{}`, strings.Repeat(" ", 1<<20) + validContext, strings.Replace(validContext, `"envCode":"dev"`, `"envCode":"foreign","envCode":"dev"`, 1), strings.Replace(validContext, `"relatedResources":[]`, `"relatedResources":[{"kind":"application","code":"a"},{"kind":"application","code":"b"}]`, 1)} {
		c, e := NewCatalog("https://catalog.internal", "svc", "secret", &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = c.Context(context.Background(), "dev", "db-proxy", "proxy"); e == nil {
			t.Fatalf("accepted malformed context %.200s", body)
		}
	}
}
func TestCatalogRedirectAndContentTypeDenied(t *testing.T) {
	for _, mode := range []string{"redirect", "html", "unauthorized"} {
		calls := 0
		c, _ := NewCatalog("https://catalog.internal", "svc", "secret", &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			calls++
			status := 200
			h := http.Header{"Content-Type": []string{"application/json"}}
			switch mode {
			case "redirect":
				status = 302
				h.Set("Location", "https://foreign.invalid")
			case "html":
				h.Set("Content-Type", "text/html")
			case "unauthorized":
				status = 401
			}
			return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(validContext))}, nil
		})})
		if _, e := c.Context(context.Background(), "dev", "db-proxy", "proxy"); e == nil || calls != 1 {
			t.Fatal(mode, e, calls)
		}
	}
}
func TestCatalogSelectorsAndOriginFailBeforeTransport(t *testing.T) {
	for _, origin := range []string{"http://remote.invalid", "https://u:p@catalog.internal", "https://catalog.internal/path", "https://catalog.internal?x=1", "https://catalog.internal#fragment"} {
		if _, e := NewCatalog(origin, "svc", "secret", nil); e == nil {
			t.Fatal("accepted origin", origin)
		}
	}
	c, _ := NewCatalog("https://catalog.internal", "svc", "secret", &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid selector reached transport")
		return nil, nil
	})})
	for _, code := range []string{"", "https://foreign", "x?y", " x"} {
		if _, e := c.Context(context.Background(), "dev", "db-proxy", code); e == nil {
			t.Fatal("accepted selector", code)
		}
	}
}

func TestCatalogRequiresAllProjectionFieldsAndConsistentACK(t *testing.T) {
	for _, body := range []string{strings.Replace(validContext, `"clusterId":"ack",`, "", 1), strings.Replace(validContext, `"clusterId":"ack"`, `"clusterId":"ack","ackClusterId":"foreign"`, 1)} {
		c, _ := NewCatalog("https://catalog.internal", "svc", "secret", &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		if _, e := c.Context(context.Background(), "dev", "db-proxy", "proxy"); e == nil {
			t.Fatal("accepted missing projection or conflicting ACK", body)
		}
	}
}

func TestCatalogCanonicalCodeAndEmptyClusterForProxyRead(t *testing.T) {
	body := strings.Replace(validContext, `"clusterId":"ack"`, `"clusterId":""`, 1)
	c, _ := NewCatalog("https://catalog.internal", "svc", "secret", &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("code") != "alias" {
			t.Fatal("alias lost")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	out, e := c.Context(context.Background(), "dev", "db-proxy", "alias")
	if e != nil || out.Code != "proxy" || out.ClusterID != "" {
		t.Fatal(out, e)
	}
}
