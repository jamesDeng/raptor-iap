// mcp-probe verifies the protected endpoint without invoking a model.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type transport struct{ username, password string }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth(t.username, t.password)
	r.Header.Set("X-Infra-Authorization", r.Header.Get("Authorization"))
	r.Header.Del("Authorization")
	return http.DefaultTransport.RoundTrip(r)
}
func run() error {
	endpoint := os.Getenv("INFRA_MCP_URL")
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid HTTPS endpoint")
	}
	user, pass := os.Getenv("INFRA_USERNAME"), os.Getenv("INFRA_PASSWORD")
	if user == "" || pass == "" {
		return errors.New("missing caller credentials")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "infra-mcp-probe", Version: "1"}, nil)
	httpClient := &http.Client{Transport: transport{user, pass}, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	s, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if e != nil {
		return errors.New("MCP initialization failed")
	}
	defer s.Close()
	list, e := s.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil || validateProbeCatalog(list.Tools) != nil {
		return errors.New("unexpected tool catalog")
	}
	args := map[string]any{"envCode": os.Getenv("INFRA_ENV_CODE"), "kind": "application", "code": os.Getenv("INFRA_APP_CODE")}
	result, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "deployments_list", Arguments: args})
	if e != nil || result.IsError {
		return errors.New("MCP deployment read failed")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"toolCount": len(list.Tools), "deployments": result.StructuredContent})
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

// The probe checks the active resource contract, including optional selection.
// Account verification is internal; a cloud-identity tool is a legacy mismatch.
func validateProbeCatalog(tools []*mcp.Tool) error {
	fields := map[string][]string{
		"deployments_list": {"envCode", "kind", "code"}, "deployment_status_get": {"envCode", "appCode"}, "proxy_observation_get": {"requestId", "envCode", "proxyCode", "kind"}, "proxy_pgcat_metrics_get": {"requestId", "envCode", "proxyCode", "series"},
		"db_proxy_scale": {"requestId", "actionId", "envCode", "proxyCode", "desiredCapacity"}, "db_proxy_node_protection_set": {"requestId", "envCode", "proxyCode", "nodeIds", "protected"}, "db_proxy_nodes_deregister": {"requestId", "envCode", "proxyCode", "nodeIds"}, "deployment_restart": {"requestId", "envCode", "appCode", "workloadKey"},
	}
	invalid := errors.New("unexpected resource tool catalog")
	seen := map[string]bool{}
	sorted := func(keys []string) string {
		copy := append([]string{}, keys...)
		sort.Strings(copy)
		return strings.Join(copy, ",")
	}
	for _, tool := range tools {
		if tool == nil || seen[tool.Name] || fields[tool.Name] == nil {
			return invalid
		}
		seen[tool.Name] = true
		var schema struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type  string `json:"type"`
				Items struct {
					Type string `json:"type"`
				} `json:"items"`
			} `json:"properties"`
			Required             []string `json:"required"`
			AdditionalProperties *bool    `json:"additionalProperties"`
		}
		raw, e := json.Marshal(tool.InputSchema)
		if e != nil || json.Unmarshal(raw, &schema) != nil || schema.Type != "object" || schema.AdditionalProperties == nil || *schema.AdditionalProperties || sorted(schema.Required) != sorted(fields[tool.Name]) {
			return invalid
		}
		expected := append([]string{}, fields[tool.Name]...)
		if tool.Name == "deployment_status_get" {
			expected = append(expected, "workloadKey")
		}
		keys := []string{}
		for key, property := range schema.Properties {
			keys = append(keys, key)
			want := "string"
			switch key {
			case "desiredCapacity":
				want = "integer"
			case "protected":
				want = "boolean"
			case "nodeIds":
				want = "array"
			}
			if property.Type != want || (want == "array" && property.Items.Type != "string") {
				return invalid
			}
		}
		if sorted(keys) != sorted(expected) {
			return invalid
		}
	}
	for _, name := range []string{"deployments_list", "deployment_status_get", "proxy_observation_get", "proxy_pgcat_metrics_get"} {
		if !seen[name] {
			return invalid
		}
	}
	return nil
}
