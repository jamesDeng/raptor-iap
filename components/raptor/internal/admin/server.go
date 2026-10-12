package admin

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type adminPage struct{ title, description, panel, script, refresh string }

var adminPages = map[string]adminPage{
	"/users":              {"Users", "Manage platform accounts and access levels.", usersPanel, "", ""},
	"/environment-groups": {"Environment groups", "Organize deployment environments under platform groups.", groupPanel, environmentScript, "refreshEnvironments()"},
	"/environments":       {"Environments", "Manage cloud targets and source repositories for each stage.", environmentListPanel, environmentScript, "refreshEnvironmentPage()"},
	"/ai-models":          {"AI models", "Connect model providers and configure available models.", `<div id="model-admin"></div>`, modelScript, "refreshModelProviders()"},
	"/github-pat":         {"GitHub token", "Manage the credential for repository change attempts.", githubPATPanel, githubPATScript, "refreshGitHubPAT()"},
}

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
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Raptor Admin</title></head><body><script>const routes={'#users-section':'/users','#groups-section':'/environment-groups','#environments-section':'/environments','#model-admin':'/ai-models'};location.replace(routes[location.hash]||'/users')</script><a href="/users">Continue to Users</a></body></html>`))
			return
		}
		config, ok := adminPages[r.URL.Path]
		if r.URL.Path == "/environments" && r.URL.Query().Get("mode") == "create" {
			config = adminPage{"Create environment", "Set the cloud target and source repositories.", environmentFormPanel, environmentScript, "refreshEnvironmentPage()"}
		}
		if !ok && strings.HasPrefix(r.URL.Path, "/environments/") {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/environments/"), "/")
			if parts[0] != "" && (len(parts) == 1 || (len(parts) == 2 && parts[1] == "edit")) {
				if len(parts) == 1 {
					config = adminPage{"Environment details", "Cloud target and source repositories.", environmentDetailPanel, environmentScript, "refreshEnvironmentPage()"}
				} else {
					config = adminPage{"Edit environment", "Update the cloud target and source repositories.", environmentFormPanel, environmentScript, "refreshEnvironmentPage()"}
				}
				ok = true
			}
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		content := strings.Replace(page, "<!-- PAGE_CONTENT -->", config.panel, 1)
		content = strings.Replace(content, "</style>", environmentStyles+"</style>", 1)
		content = strings.Replace(content, "<!-- PAGE_TITLE -->", config.title, 1)
		content = strings.Replace(content, "<!-- PAGE_DESCRIPTION -->", config.description, 1)
		activePath := r.URL.Path
		if strings.HasPrefix(activePath, "/environments/") {
			activePath = "/environments"
		}
		content = strings.Replace(content, `href="`+activePath+`"`, `href="`+activePath+`" aria-current="page"`, 1)
		content = strings.Replace(content, "<!-- PAGE_SCRIPT -->", config.script+`<script>async function refreshPage(){`+config.refresh+`}</script>`, 1)
		_, _ = w.Write([]byte(content))
	}), nil
}
