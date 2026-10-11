package main

import (
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type captureHTTP struct {
	t     *testing.T
	calls int
}

func (h *captureHTTP) Call(r *http.Request, _ *http.Transport) (*http.Response, error) {
	h.calls++
	raw, _ := io.ReadAll(r.Body)
	v, _ := url.ParseQuery(string(raw))
	if v.Get("DryRun") != "true" || v.Get("All") != "false" || v.Get("ResourceType") != "servergroup" || v.Get("ResourceId.1") != "sgp-xs5s42tihtl6ja2a67" || v.Get("TagKey.1") != "db-proxy-code" {
		h.t.Fatalf("unsafe diagnostic wire: %s", raw)
	}
	if r.URL.Host != "nlb.ap-southeast-1.aliyuncs.com" {
		h.t.Fatal("endpoint escape")
	}
	return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"Code":"DryRunOperation","Message":"permission checked","RequestId":"fixture"}`))}, nil
}
func TestPermissionProbeAlwaysDryRun(t *testing.T) {
	c, e := openapi.NewClient(&openapi.Config{AccessKeyId: dara.String("fixture"), AccessKeySecret: dara.String("fixture"), Endpoint: dara.String("nlb.ap-southeast-1.aliyuncs.com")})
	if e != nil {
		t.Fatal(e)
	}
	h := &captureHTTP{t: t}
	c.HttpClient = h
	_, _ = probe(c, "servergroup", "sgp-xs5s42tihtl6ja2a67")
	if h.calls != 1 {
		t.Fatalf("calls=%d", h.calls)
	}
}
