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

func TestESSDiscoveryFetchesFullTagsByExactID(t *testing.T) {
	for _, mode := range []string{"valid", "foreign-id", "changed-tags", "missing"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				r.ParseForm()
				detail := r.Form.Get("ScalingGroupIds.1") != ""
				if r.Form.Get("RegionId") != "ap-southeast-1" {
					t.Error("request lost environment region")
				}
				if detail {
					if r.Form.Get("ScalingGroupIds.1") != "asg-test" || r.Form.Get("ScalingGroupIds.2") != "" {
						t.Error("hydration must target exactly the discovered group")
					}
					for key := range r.Form {
						if strings.HasPrefix(key, "Tags.") {
							t.Error("hydration must not repeat tag filtering")
						}
					}
				}
				id := "asg-test"
				env := "dev"
				if detail && mode == "foreign-id" {
					id = "foreign"
				}
				if detail && mode == "changed-tags" {
					env = "foreign"
				}
				tags := []map[string]string{{"TagKey": "env", "TagValue": env}, {"TagKey": "db-proxy-code", "TagValue": "proxy"}}
				if detail {
					tags = append(tags, map[string]string{"TagKey": "target-db-code", "TagValue": "db"})
				}
				rows := []any{map[string]any{"ScalingGroupId": id, "Tags": tags}}
				if detail && mode == "missing" {
					rows = []any{}
				}
				json.NewEncoder(w).Encode(map[string]any{"TotalCount": len(rows), "ScalingGroups": rows})
			}))
			defer s.Close()
			cfg := func(string) *openapi.Config {
				return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(s.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture")}
			}
			pg, e := cloudPages(cfg)(context.Background(), domain.Environment{Code: "dev", Region: "ap-southeast-1"}, "db-proxy", "proxy", 1)
			if mode != "valid" {
				if e == nil {
					t.Fatal("changed or missing exact target accepted")
				}
				return
			}
			if e != nil || calls != 2 || len(pg.Records) != 1 || pg.Records[0].Tags["target-db-code"] != "db" {
				t.Fatal("filtered discovery lost association", e, calls)
			}
		})
	}
}
