package admin

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"

	adminfrontend "github.com/jamesDeng/raptor-iap/components/raptor/admin-frontend"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
)

var environmentPage = regexp.MustCompile(`^/environments/[A-Za-z0-9][A-Za-z0-9._-]*(/edit)?$`)

func NewHandler(backendURL string) (http.Handler, error) {
	u, e := url.Parse(backendURL)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid backend URL")
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { httpx.Error(w, 503, "BackendUnavailable") }
	files, e := fs.Sub(adminfrontend.Files, "dist")
	if e != nil {
		return nil, e
	}
	index, e := fs.ReadFile(files, "index.html")
	if e != nil {
		return nil, e
	}
	assets := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p := r.URL.Path
		if strings.HasPrefix(p, "/assets/") {
			info, err := fs.Stat(files, strings.TrimPrefix(p, "/"))
			if err != nil || info.IsDir() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			assets.ServeHTTP(w, r)
			return
		}
		known := p == "/" || p == "/users" || p == "/environment-groups" || p == "/environments" || p == "/ai-models" || p == "/github-pat" || environmentPage.MatchString(p)
		if !known {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			_, _ = w.Write(index)
		}
	}), nil
}
