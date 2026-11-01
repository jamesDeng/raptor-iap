package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

func TestRDSDiscoveryDoesNotUseEnvironmentAsAnORFilter(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Form.Get("Action") == "DescribeDBInstances" || req.Header.Get("x-acs-action") == "DescribeDBInstances" {
			var filters map[string]string
			if err := json.Unmarshal([]byte(req.Form.Get("Tags")), &filters); err != nil {
				t.Error(err)
				return
			}
			// RDS matches ANY tag: adding env also selects another database.
			rows := []any{map[string]any{"DBInstanceId": "rm-target"}}
			if filters["env"] == "dev" {
				rows = append(rows, map[string]any{"DBInstanceId": "rm-other"})
			}
			if filters["db-code"] != "db" {
				t.Error("missing exact database code")
			}
			json.NewEncoder(w).Encode(map[string]any{"TotalRecordCount": len(rows), "Items": map[string]any{"DBInstance": rows}})
			return
		}
		code := "db"
		if req.Form.Get("DBInstanceId") == "rm-other" {
			code = "other"
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": map[string]any{"TagInfos": []any{map[string]any{"TagKey": "env", "TagValue": "dev"}, map[string]any{"TagKey": "db-code", "TagValue": code}}}})
	}))
	defer s.Close()
	cfg := func(string) *openapi.Config {
		return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(s.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture")}
	}
	r := &reader{page: cloudPages(cfg), identity: func(context.Context, domain.Environment) (string, error) { return "account", nil }}
	got, err := r.ResolveResource(context.Background(), domain.ResourceContext{EnvCode: "dev", Code: "db", Kind: "database", Cloud: "aliyun", Region: "ap-southeast-1", AccountID: "account"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.DatabaseWorkloads) != 1 || got.DatabaseWorkloads[0].ResourceID != "rm-target" {
		t.Fatalf("wrong database selection: %+v", got.DatabaseWorkloads)
	}
}
