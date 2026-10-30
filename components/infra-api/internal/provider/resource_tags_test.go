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
	"time"
)

func TestResourceCloudPagesRejectMalformedTags(t *testing.T) {
	for _, kind := range []string{"db-proxy", "database"} {
		for _, mode := range []string{"duplicate", "nil", "missing"} {
			t.Run(kind+mode, func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.ParseForm()
					w.Header().Set("Content-Type", "application/json")
					if kind == "db-proxy" {
						tags := []any{map[string]any{"TagKey": "env", "TagValue": "dev"}, map[string]any{"TagKey": "db-proxy-code", "TagValue": "proxy"}, map[string]any{"TagKey": "target-db-code", "TagValue": "db"}}
						switch mode {
						case "duplicate":
							tags = append(tags, tags[0])
						case "nil":
							tags = append(tags, nil)
						case "missing":
							tags = append(tags, map[string]any{"TagKey": "broken"})
						}
						json.NewEncoder(w).Encode(map[string]any{"TotalCount": 1, "ScalingGroups": []any{map[string]any{"ScalingGroupId": "asg-test", "Tags": tags}}})
						return
					}
					if r.Form.Get("Action") == "DescribeDBInstances" || r.Header.Get("x-acs-action") == "DescribeDBInstances" {
						json.NewEncoder(w).Encode(map[string]any{"TotalRecordCount": 1, "Items": map[string]any{"DBInstance": []any{map[string]any{"DBInstanceId": "rm-db"}}}})
						return
					}
					tags := []any{map[string]any{"TagKey": "env", "TagValue": "dev"}, map[string]any{"TagKey": "db-code", "TagValue": "db"}}
					switch mode {
					case "duplicate":
						tags = append(tags, tags[0])
					case "nil":
						tags = append(tags, nil)
					case "missing":
						tags = append(tags, map[string]any{"TagKey": "broken"})
					}
					json.NewEncoder(w).Encode(map[string]any{"Items": map[string]any{"TagInfos": tags}})
				}))
				defer s.Close()
				cfg := func(string) *openapi.Config {
					return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(s.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture")}
				}
				code := "proxy"
				if kind == "database" {
					code = "db"
				}
				if _, e := cloudPages(cfg)(context.Background(), domain.Environment{Code: "dev", Region: "ap-southeast-1"}, kind, code, 1); e == nil {
					t.Fatal("malformed tag metadata accepted")
				}
			})
		}
	}
}
func TestResourceRDSDeadlineBoundsLegacySDKCall(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"TotalRecordCount":0,"Items":{"DBInstance":[]}}`))
	}))
	defer s.Close()
	cfg := func(string) *openapi.Config {
		return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(s.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, e := cloudPages(cfg)(ctx, domain.Environment{Code: "dev", Region: "ap-southeast-1"}, "database", "db", 1)
	if e == nil || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("legacy RDS call ignored deadline: %v %s", e, time.Since(start))
	}
}
