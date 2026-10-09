package requests

import (
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
	"strconv"
)

func (s *Service) registerMessages(m *http.ServeMux, a *auth.Service) {
	m.Handle("POST /api/v1/requests/{id}/messages", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			MessageID string `json:"messageId"`
			Text      string `json:"text"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		v, e := s.SubmitMessage(r.Context(), auth.UserFrom(r), r.PathValue("id"), in.MessageID, in.Text)
		if errors.Is(e, ErrMessageCapacity) {
			httpx.Error(w, 429, "MessageCapacity")
			return
		}
		httpx.Result(w, 202, v, e)
	})))
	m.Handle("GET /api/v1/requests/{id}/messages", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var after int64
		var e error
		if r.URL.Query().Has("afterInputSequence") {
			after, e = strconv.ParseInt(r.URL.Query().Get("afterInputSequence"), 10, 64)
		}
		if e != nil {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		v, e := s.ListMessages(r.Context(), r.PathValue("id"), after)
		httpx.Result(w, 200, v, e)
	})))
}
