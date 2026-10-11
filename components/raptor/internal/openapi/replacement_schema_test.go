package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReplacementRaptorSchemasMatchHarness(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required for producer/consumer contract test")
	}
	ctx := context.Background()
	server := NewMCP(Client{})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "contract", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{"request_get": true, "environment_get": true, "object_get": true, "approval_request": true, "approval_get": true, "request_pause": true}
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
	cmd := exec.Command(node, "--input-type=module", "-e", `import fs from 'node:fs';import {pathToFileURL} from 'node:url';const {validateOperationToolSchema}=await import(pathToFileURL(process.argv[1]));const schemas=JSON.parse(fs.readFileSync(0,'utf8'));for(const [name,schema] of Object.entries(schemas))validateOperationToolSchema('raptor',name,schema);`, harness)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("producer/consumer mismatch: %v %s", err, out)
	}
}
