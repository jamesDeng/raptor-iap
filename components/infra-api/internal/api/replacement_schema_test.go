package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"raptor-iap/infra-api/internal/domain"
	"testing"
)

func TestReplacementInfraSchemasMatchHarness(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required for producer/consumer contract test")
	}
	ctx := context.Background()
	h, err := NewWithCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", &commandFixture{}, allowCommands{})
	if err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(h)
	defer web.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "contract", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: web.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpAuthTransport{}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{"cloud_identity_get": true, "deployments_list": true, "deployment_status_get": true, "deployment_restart": true, "db_proxy_scale": true, "db_proxy_node_protection_set": true, "db_proxy_nodes_deregister": true}
	schemas := map[string]any{}
	for _, tool := range list.Tools {
		if names[tool.Name] {
			schemas[tool.Name] = tool.InputSchema
		}
	}
	if len(schemas) != len(names) {
		t.Fatal("missing required producer tools")
	}
	data, err := json.Marshal(schemas)
	if err != nil {
		t.Fatal(err)
	}
	harness, err := filepath.Abs("../../../agent-harness/operation-profile.mjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", `import fs from 'node:fs';import {pathToFileURL} from 'node:url';const {validateOperationToolSchema}=await import(pathToFileURL(process.argv[1]));const schemas=JSON.parse(fs.readFileSync(0,'utf8'));for(const [name,schema] of Object.entries(schemas))validateOperationToolSchema('infra',name,schema);`, harness)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("producer/consumer mismatch: %v %s", err, out)
	}
}
