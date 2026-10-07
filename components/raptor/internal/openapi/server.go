package openapi

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

func NewHandler(c Client) (http.Handler, error) {
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Host == "" || u.User != nil {
		return nil, url.InvalidHostError("invalid backend")
	}
	server := NewMCP(c)
	m := http.NewServeMux()
	m.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	proxy := httputil.NewSingleHostReverseProxy(u)
	director := proxy.Director
	proxy.Director = func(r *http.Request) { director(r); r.SetBasicAuth(c.Username, c.Password) }
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { httpx.Error(w, 503, "Unavailable") }
	m.Handle("/v1/", proxy)
	service := auth.BasicAuth(m, auth.Credentials{Username: c.Username, Password: c.Password})
	inspector := Client{BaseURL: c.BaseURL, AllowClusterHTTP: c.AllowClusterHTTP, Username: os.Getenv("AGENT_INTROSPECTION_USERNAME"), Password: os.Getenv("AGENT_INTROSPECTION_PASSWORD")}
	privateProxy := httputil.NewSingleHostReverseProxy(u)
	privateDirector := privateProxy.Director
	privateProxy.Director = func(r *http.Request) { privateDirector(r); r.SetBasicAuth(inspector.Username, inspector.Password) }
	privateProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { httpx.Error(w, 503, "Unavailable") }
	privateRoute := auth.BasicAuth(privateProxy, auth.Credentials{Username: inspector.Username, Password: inspector.Password})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if len(r.Header.Values("Authorization")) > 1 {
			httpx.Error(w, 401, "Unauthenticated")
			return
		}
		parts := strings.Fields(header)
		if len(parts) > 0 && strings.EqualFold(parts[0], "Bearer") {
			if len(parts) != 2 {
				httpx.Error(w, 401, "Unauthenticated")
				return
			}
			agentHandler(c, inspector, parts[1]).ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/v1/agent-access/introspect" {
			if r.Method != "POST" {
				httpx.Error(w, 405, "MethodNotAllowed")
				return
			}
			privateRoute.ServeHTTP(w, r)
			return
		}
		service.ServeHTTP(w, r)
	}), nil
}
