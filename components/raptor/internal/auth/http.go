package auth

import (
	"context"
	"crypto/subtle"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

type userKey struct{}

func UserFrom(r *http.Request) domain.User {
	u, _ := r.Context().Value(userKey{}).(domain.User)
	return u
}
func BasicAuth(next http.Handler, c Credentials) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || c.Username == "" || c.Password == "" || subtle.ConstantTimeCompare([]byte(u), []byte(c.Username)) != 1 || subtle.ConstantTimeCompare([]byte(p), []byte(c.Password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="raptor-service"`)
			httpx.Error(w, 401, "Unauthenticated")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Service) Browser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, e := r.Cookie("raptor_session")
		if e != nil {
			httpx.Error(w, 401, "Unauthenticated")
			return
		}
		u, e := s.Authenticate(r.Context(), cookie.Value)
		if e != nil {
			httpx.Error(w, 401, "Unauthenticated")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			var h string
			e = s.Pool.QueryRow(r.Context(), "SELECT csrf_hash FROM raptor.sessions WHERE token_hash=$1", digest(cookie.Value)).Scan(&h)
			if e != nil || subtle.ConstantTimeCompare([]byte(h), []byte(digest(r.Header.Get("X-CSRF-Token")))) != 1 {
				httpx.Error(w, 403, "CSRFRequired")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}
func (s *Service) Admin(next http.Handler) http.Handler {
	return s.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequireAdmin(UserFrom(r)) != nil {
			httpx.Error(w, 403, "Forbidden")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
func (s *Service) Register(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		sess, e := s.Login(r.Context(), in.Username, in.Password)
		if e != nil {
			httpx.Error(w, 401, "Unauthenticated")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "raptor_session", Value: sess.Token, Path: "/", HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 28800})
		u, _ := s.Authenticate(r.Context(), sess.Token)
		httpx.Write(w, 200, map[string]any{"user": u, "csrf": sess.CSRF})
	})
	m.Handle("GET /api/v1/session", s.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("raptor_session")
		httpx.Write(w, 200, map[string]any{"user": UserFrom(r), "csrf": csrf(c.Value)})
	})))
	m.Handle("POST /api/v1/logout", s.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("raptor_session")
		if s.Logout(r.Context(), c.Value) != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "raptor_session", Path: "/", HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		httpx.Write(w, 200, map[string]bool{"loggedOut": true})
	})))
	m.Handle("GET /api/v1/users", s.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, e := s.ListUsers(r.Context())
		if e != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		httpx.Write(w, 200, u)
	})))
	m.Handle("POST /api/v1/users", s.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		u, e := s.CreateUser(r.Context(), UserFrom(r), in.Username, in.Password, in.Role)
		if e != nil {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		httpx.Write(w, 201, u)
	})))
}
