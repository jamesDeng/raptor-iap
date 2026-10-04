package backend

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	githubservice "github.com/jamesDeng/raptor-iap/components/raptor/internal/github"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"net/http"
)

type Server struct {
	Mux       *http.ServeMux
	Auth      *auth.Service
	Catalog   *catalog.Service
	Requests  *requests.Service
	Approvals *approvals.Service
	GitHub    *githubservice.Service
}

func New(p *pgxpool.Pool) *Server {
	s := &Server{Mux: http.NewServeMux(), Auth: auth.NewService(p)}
	s.Auth.Register(s.Mux)
	s.Catalog = catalog.NewService(p, adapters.UnavailableInfra{})
	s.Catalog.Register(s.Mux, s.Auth)
	s.Requests = requests.NewService(p, s.Catalog)
	s.Requests.Register(s.Mux, s.Auth)
	s.Approvals = &approvals.Service{Pool: p}
	s.Approvals.RegisterBrowser(s.Mux, s.Auth)
	s.GitHub = &githubservice.Service{Pool: p}
	s.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.Mux.ServeHTTP(w, r) }
