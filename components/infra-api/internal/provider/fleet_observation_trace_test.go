package provider

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

func TestFleetReadTracesActualESSErrorWithoutRawResponse(t *testing.T) {
	const providerID = "01A23BCD-4567-89AB-CDEF-0123456789AB"
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-acs-action") != "DescribeScalingInstances" {
			t.Error("unexpected ESS operation")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"Code":"Throttling","Message":"raw-provider-secret","RequestId":"` + providerID + `"}`))
	}))
	defer server.Close()
	sdk, err := cloudProxy(func(string) *openapi.Config {
		return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(server.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("credential-secret")}
	})
	if err != nil {
		t.Fatal(err)
	}
	p, env, _, _, _, _ := proxyFixture(t)
	p.sdk.instances = sdk.instances
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	const observationID = "5c222020-6d33-4fd8-ad43-1adb377a0c54"
	ctx := domain.WithFleetReadTrace(context.Background(), "8a0cb7f0-4ea3-4940-b9dc-6eec1292f194", observationID, "")
	if _, err := p.observationMembers(ctx, env, env.Proxies[0]); err == nil {
		t.Fatal("failed provider read succeeded")
	}
	if calls != 1 {
		t.Fatalf("sent %d ESS calls", calls)
	}
	for _, field := range []string{"stage=ess_scaling_instances", "category=throttled", "elapsed_ms=", "upstream_request_id=" + providerID, "observation_id=" + observationID} {
		if !strings.Contains(logs.String(), field) {
			t.Errorf("missing %s in %s", field, logs.String())
		}
	}
	if strings.Contains(logs.String(), "raw-provider-secret") || strings.Contains(logs.String(), "credential-secret") {
		t.Fatal("raw provider data leaked into logs")
	}
}
