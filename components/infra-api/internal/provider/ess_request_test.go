package provider

import (
	"context"
	"encoding/json"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

func TestESSActualSDKRequest(t *testing.T) {
	seen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("PageSize") != "50" {
			t.Errorf("unsupported ESS PageSize: %s", r.Form.Get("PageSize"))
		}
		if r.Form.Get("PageNumber") != "2" || r.Form.Get("RegionId") != "ap-southeast-1" {
			t.Error("incorrect page or region")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"TotalCount": 0, "ScalingGroups": []any{}, "RequestId": "fixture"})
	}))
	defer server.Close()
	config := func(string) *openapi.Config {
		return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(server.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture")}
	}
	_, err := cloudPages(config)(context.Background(), domain.Environment{Code: "dev", Region: "ap-southeast-1"}, "db-proxy", "proxy", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("SDK did not send request")
	}
}

func TestACKActualSDKRequest(t *testing.T) {
	seen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"config":"invalid fixture"}`))
	}))
	defer server.Close()
	config := func(string) *openapi.Config {
		return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(server.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture")}
	}
	_, err := cloudKube(config)(context.Background(), domain.Environment{Code: "dev", Region: "ap-southeast-1", ClusterID: "fixture"})
	if !seen {
		t.Fatal("SDK did not send request")
	}
	if err == nil {
		t.Fatal("invalid kubeconfig accepted")
	}
}
