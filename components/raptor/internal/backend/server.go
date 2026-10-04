package backend

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"net/http"
)

type Server struct {
	Mux  *http.ServeMux
	Auth *auth.Service
}

func New(p *pgxpool.Pool) *Server {
	s := &Server{Mux: http.NewServeMux(), Auth: auth.NewService(p)}
	s.Auth.Register(s.Mux)
	s.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.Mux.ServeHTTP(w, r) }
