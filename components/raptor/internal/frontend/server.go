package frontend

import (
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"github.com/jamesDeng/raptor-iap/components/raptor/web"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
)

func NewHandler(backendURL string) (http.Handler, error) {
	u, e := url.Parse(backendURL)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid backend")
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { httpx.Error(w, 503, "Unavailable") }
	files, e := fs.Sub(web.Files, "poc")
	if e != nil {
		return nil, e
	}
	static := http.FileServer(http.FS(files))
	connectOrigin := os.Getenv("RAPTOR_PROGRESS_CONNECT_ORIGIN")
	if connectOrigin != "" {
		v, e := url.Parse(connectOrigin)
		if e != nil || v.Host == "" || v.User != nil || v.Path != "" || v.RawQuery != "" || v.Fragment != "" || strings.ContainsAny(connectOrigin, " *;\t\r\n") || !(v.Scheme == "wss" || (v.Scheme == "ws" && (v.Hostname() == "localhost" || v.Hostname() == "127.0.0.1" || v.Hostname() == "::1"))) {
			return nil, fmt.Errorf("invalid Gateway connect origin")
		}
	}
	connectSource := "'self'"
	if connectOrigin != "" {
		connectSource += " " + connectOrigin
	}
	public := os.Getenv("RAPTOR_PUBLIC_FRONTEND") == "true"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src "+connectSource+"; frame-ancestors 'none'")
		clean := path.Clean(r.URL.Path)
		users := clean == "/api/v1/users" || strings.HasPrefix(clean, "/api/v1/users/")
		envWrite := r.Method != "GET" && r.Method != "HEAD" && (clean == "/api/v1/environments" || strings.HasPrefix(clean, "/api/v1/environments/") || clean == "/api/v1/environment-groups" || strings.HasPrefix(clean, "/api/v1/environment-groups/"))
		modelAdmin := clean == "/api/v1/admin/model-providers" || strings.HasPrefix(clean, "/api/v1/admin/model-providers/")
		if public && (users || envWrite || modelAdmin) {
			httpx.Error(w, http.StatusForbidden, "PrivateManagementRequired")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		static.ServeHTTP(w, r)
	}), nil
}
