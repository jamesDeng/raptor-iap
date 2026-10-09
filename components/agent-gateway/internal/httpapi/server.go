package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net/http"
	"strconv"
)

func New(s *execution.Store, user, password string, options ...ProgressConfig) http.Handler {
	m := http.NewServeMux()
	cfg := ProgressConfig{}
	if len(options) > 0 {
		cfg = options[0]
	}
	progress := newProgress(s, cfg)
	m.HandleFunc("POST /v1/requests/{id}/progress-ticket", progress.mint)
	m.HandleFunc("POST /v1/requests/{id}/signals", func(w http.ResponseWriter, r *http.Request) {
		var v execution.Signal
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		d.DisallowUnknownFields()
		if d.Decode(&v) != nil {
			write(w, 400, map[string]string{"error": "InvalidInput"})
			return
		}
		v.RequestID = r.PathValue("id")
		e := s.DeliverSignal(r.Context(), v)
		result(w, map[string]bool{"accepted": e == nil}, e)
	})
	m.HandleFunc("PUT /v1/requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RequestID string `json:"requestId"`
			Type      string `json:"type"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || in.RequestID != r.PathValue("id") || in.Type != "agent" {
			write(w, 400, map[string]string{"error": "InvalidInput"})
			return
		}
		v, e := s.Receive(r.Context(), in.RequestID)
		result(w, v, e)
	})
	m.HandleFunc("GET /v1/requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Get(r.Context(), r.PathValue("id"))
		result(w, v, e)
	})
	m.HandleFunc("GET /v1/requests/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		after := int64(0)
		var e error
		if r.URL.Query().Has("after") {
			after, e = strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		}
		if e != nil || after < 0 {
			write(w, 400, map[string]string{"error": "InvalidInput"})
			return
		}
		v, e := s.Events(r.Context(), r.PathValue("id"), after)
		if e != nil {
			result(w, nil, e)
			return
		}
		more := len(v) > 100
		if more {
			v = v[:100]
		}
		next := after
		if len(v) > 0 {
			next = v[len(v)-1].Sequence
		}
		write(w, 200, map[string]any{"data": v, "nextAfter": next, "hasMore": more})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/v1/progress" {
			progress.connect(w, r)
			return
		}
		u, p, ok := r.BasicAuth()
		if !ok || user == "" || password == "" || subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 || subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
			write(w, 401, map[string]string{"error": "Unauthorized"})
			return
		}
		m.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func result(w http.ResponseWriter, v any, e error) {
	if e != nil {
		write(w, 503, map[string]string{"error": "Unavailable"})
		return
	}
	write(w, 200, map[string]any{"data": v})
}
