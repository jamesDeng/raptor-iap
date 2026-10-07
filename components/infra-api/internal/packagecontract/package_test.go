package packagecontract

import (
	"os"
	"strings"
	"testing"
)

func TestFC3PackageBoundary(t *testing.T) {
	b, e := os.ReadFile("../../s.yaml")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, v := range []string{"component: fc3", "region: ap-southeast-1", "runtime: custom.debian10", "memorySize: 512", "timeout: 30", "instanceConcurrency: 2", "concurrencyConfig:\n        reservedConcurrency: 2", "port: 9000", "./server", "authType: function", "X-Infra-Authorization", "code: .build"} {
		if !strings.Contains(s, v) {
			t.Fatal("missing " + v)
		}
	}
	for _, v := range []string{"authType: anonymous", "provisionConfig", "127.0.0.1", "accessKeySecret:"} {
		if strings.Contains(s, v) {
			t.Fatal("unsafe " + v)
		}
	}
}

func TestMCPDeploymentBoundary(t *testing.T) {
	for _, path := range []string{"../../s.yaml", "../../s.rdev.yaml"} {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(b), "methods: [GET, POST]") || !strings.Contains(string(b), "authType: function") {
			t.Fatal("MCP requires protected POST trigger")
		}
	}
	b, e := os.ReadFile("../../../../terraform-module/infra-api-read/main.tf")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, v := range []string{`resource "alicloud_api_gateway_api" "mcp"`, `method   = "POST"`, `path     = "/mcp"`, `mode     = "PASSTHROUGH"`} {
		if !strings.Contains(s, v) {
			t.Fatal("missing MCP route: " + v)
		}
	}
}
