package admin

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func NewHandler(backendURL string) (http.Handler, error) {
	u, e := url.Parse(backendURL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmtURL()
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { httpx.Error(w, 503, "BackendUnavailable") }
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}), nil
}
