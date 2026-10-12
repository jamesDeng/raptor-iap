package githubcredentials

import (
	"errors"
	"net/http"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
)

type HTTPService struct{ Service *Service }

func (h *HTTPService) Register(m *http.ServeMux, a *auth.Service, gateway auth.Credentials) {
	m.Handle("GET /api/v1/admin/github-pat", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, err := h.Service.Status(r.Context())
		httpx.Result(w, 200, status, err)
	})))
	m.Handle("PUT /api/v1/admin/github-pat", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token string `json:"token"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		status, err := h.Service.Save(r.Context(), auth.UserFrom(r).ID, in.Token)
		if errors.Is(err, ErrInvalid) {
			httpx.Error(w, 400, "InvalidCredential")
			return
		}
		if err != nil {
			httpx.Error(w, 503, "CredentialUnavailable")
			return
		}
		httpx.Write(w, 200, status)
	})))
	m.Handle("DELETE /api/v1/admin/github-pat", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h.Service.Delete(r.Context(), auth.UserFrom(r).ID); err != nil {
			httpx.Error(w, 503, "CredentialUnavailable")
			return
		}
		httpx.Write(w, 200, map[string]bool{"deleted": true})
	})))
	m.Handle("GET /v1/private/github-pat", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		secret, err := h.Service.Load(r.Context())
		if err != nil {
			httpx.Error(w, 503, "CredentialUnavailable")
			return
		}
		httpx.Write(w, 200, map[string]string{"token": string(secret)})
	}), gateway))
}
