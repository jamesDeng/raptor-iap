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
		content := strings.Replace(page, `<div id="environment-admin"></div>`, `<div id="environment-admin">`+environmentPanel+`</div>`, 1)
		content = strings.Replace(content, "await refresh()", "await refresh();await refreshEnvironments();await refreshModelProviders()", -1)
		content = strings.Replace(content, "refresh().catch(()=>{});", "refresh().then(refreshModelProviders).catch(()=>{});", 1)
		content = strings.Replace(content, environmentPanel+`</div>`, environmentPanel+`</div><div id="model-admin"></div>`, 1)
		content = strings.Replace(content, "</body>", environmentScript+"</body>", 1)
		content = strings.Replace(content, "</body>", modelScript+"</body>", 1)
		_, _ = w.Write([]byte(content))
	}), nil
}
