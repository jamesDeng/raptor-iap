package packagecontract

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFC3PackageBoundary(t *testing.T) {
	b, e := os.ReadFile("../../s.yaml")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, v := range []string{"component: fc3", "region: ap-southeast-1", "runtime: custom.debian10", "memorySize: 512", "timeout: 30", "instanceConcurrency: 2", "concurrencyConfig:\n        reservedConcurrency: 2", "port: 9000", "./server", "authType: function", "X-Infra-Authorization", "code: .build-resource"} {
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
	for _, v := range []string{`resource "alicloud_api_gateway_api" "mcp"`, `content_type_category = "CLIENT"`, `method   = "POST"`, `path     = "/mcp"`, `mode     = "PASSTHROUGH"`} {
		if !strings.Contains(strings.Join(strings.Fields(s), " "), strings.Join(strings.Fields(v), " ")) {
			t.Fatal("missing MCP route: " + v)
		}
	}
}

func TestResourcePackageHasNoScopeFileAuthority(t *testing.T) {
	for _, path := range []string{"../../Makefile", "../../s.yaml", "../../s.rdev.yaml", "../../s.rdev-proxy.yaml", "../../cmd/server/main.go"} {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		for _, forbidden := range []string{"INFRA_SCOPE_FILE", "scope.json", "scope.Load", "api.NewAgentInspector("} {
			if strings.Contains(string(b), forbidden) {
				t.Errorf("%s retains %s", path, forbidden)
			}
		}
	}
	for _, path := range []string{"../../s.yaml", "../../s.rdev.yaml", "../../s.rdev-proxy.yaml"} {
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), "RAPTOR_CATALOG_ORIGIN:") {
			t.Errorf("%s missing catalog origin", path)
		}
	}
}

func TestPackageBuildPreservesAndRejectsUnexpectedFiles(t *testing.T) {
	directory := t.TempDir()
	makefile, e := os.ReadFile("../../Makefile")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(directory, "Makefile"), makefile, 0600); e != nil {
		t.Fatal(e)
	}
	packageDir := filepath.Join(directory, ".build-resource")
	if e = os.Mkdir(packageDir, 0700); e != nil {
		t.Fatal(e)
	}
	unexpected := filepath.Join(packageDir, "unexpected.json")
	content := []byte("synthetic unrelated artifact")
	if e = os.WriteFile(unexpected, content, 0600); e != nil {
		t.Fatal(e)
	}
	command := exec.Command("make", "build")
	command.Dir = directory
	output, e := command.CombinedOutput()
	if e == nil || !strings.Contains(string(output), "must contain only server") {
		t.Fatal("unsafe package accepted", string(output), e)
	}
	got, e := os.ReadFile(unexpected)
	if e != nil || !bytes.Equal(got, content) {
		t.Fatal("unrelated artifact changed", e)
	}
}
