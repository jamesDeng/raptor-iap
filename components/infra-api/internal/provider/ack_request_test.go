package provider

import (
	"context"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"net/http"
	"net/http/httptest"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"testing"
)

func TestACKRequestsPrivateTemporaryKubeconfig(t *testing.T) {
	var query url.Values
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, path = r.URL.Query(), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// Return expired configuration to stop before any Kubernetes connection.
		_, _ = w.Write([]byte(`{"config":"","expiration":"2000-01-01T00:00:00Z"}`))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	factory := cloudKube(func(endpoint string) *openapi.Config {
		if endpoint != "cs.ap-southeast-1.aliyuncs.com" {
			t.Errorf("unexpected ACK endpoint %s", endpoint)
		}
		return &openapi.Config{Endpoint: tea.String(u.Host), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture")}
	})
	_, err := factory(context.Background(), domain.Environment{ClusterID: "owned-cluster"})
	if err == nil {
		t.Fatal("expired configuration accepted")
	}
	if path != "/k8s/owned-cluster/user_config" {
		t.Fatalf("wrong cluster request: %s", path)
	}
	if query.Get("PrivateIpAddress") != "true" {
		t.Fatalf("private endpoint not requested: %v", query)
	}
	if query.Get("TemporaryDurationMinutes") != "15" {
		t.Fatalf("temporary duration changed: %v", query)
	}
}
