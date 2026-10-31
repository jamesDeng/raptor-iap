package backend

import (
	"net/http"
	"net/url"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
)

func (s *Server) infraResourceContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		httpx.Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	selectors, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(selectors) != 3 {
		httpx.Error(w, http.StatusBadRequest, "InvalidInput")
		return
	}
	for _, key := range []string{"envCode", "kind", "code"} {
		values := selectors[key]
		if len(values) != 1 || values[0] == "" {
			httpx.Error(w, http.StatusBadRequest, "InvalidInput")
			return
		}
	}
	value, err := s.Catalog.InfraResourceContext(r.Context(), selectors.Get("envCode"), selectors.Get("kind"), selectors.Get("code"))
	httpx.Result(w, http.StatusOK, value, err)
}
