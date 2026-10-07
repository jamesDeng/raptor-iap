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
	if e != nil || len(list.Tools) != 3 {
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
