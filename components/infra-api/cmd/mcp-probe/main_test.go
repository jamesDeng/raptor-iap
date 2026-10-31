package main

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func probeTools() []*mcp.Tool {
	var tools []*mcp.Tool
	for name, fields := range map[string][]string{"deployments_list": {"envCode", "kind", "code"}, "deployment_status_get": {"envCode", "appCode"}, "proxy_observation_get": {"requestId", "envCode", "proxyCode", "kind"}, "proxy_pgcat_metrics_get": {"requestId", "envCode", "proxyCode", "series"}} {
		props := map[string]any{}
		for _, field := range fields {
			props[field] = map[string]any{"type": "string"}
		}
		if name == "deployment_status_get" {
			props["workloadKey"] = map[string]any{"type": "string"}
		}
		tools = append(tools, &mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object", "properties": props, "required": fields, "additionalProperties": false}})
	}
	return tools
}
func TestProbeCatalogRequiresCurrentLogicalReadContracts(t *testing.T) {
	if err := validateProbeCatalog(probeTools()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"cloud", "duplicate", "provider-selector", "required-key"} {
		tools := probeTools()
		switch kind {
		case "cloud":
			tools = append(tools, &mcp.Tool{Name: "cloud_identity_get"})
		case "duplicate":
			tools = append(tools, tools[0])
		case "provider-selector":
			schema := tools[0].InputSchema.(map[string]any)
			schema["properties"].(map[string]any)["groupId"] = map[string]any{"type": "string"}
		case "required-key":
			for _, tool := range tools {
				if tool.Name == "deployment_status_get" {
					tool.InputSchema.(map[string]any)["required"] = []string{"envCode", "appCode", "workloadKey"}
				}
			}
		}
		if validateProbeCatalog(tools) == nil {
			t.Fatal("unsafe probe catalog accepted", kind)
		}
	}
}
