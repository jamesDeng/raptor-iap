package openapi

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	return auth.BasicAuth(m, auth.Credentials{Username: c.Username, Password: c.Password}), nil
}
