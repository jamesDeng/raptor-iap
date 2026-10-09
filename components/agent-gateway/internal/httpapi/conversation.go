package httpapi

import (
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net/http"
)

func registerConversation(m *http.ServeMux, s *execution.Store) {
	m.HandleFunc("GET /v1/requests/{id}/conversation", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Conversation(r.Context(), r.PathValue("id"))
		result(w, v, e)
	})
	for _, suffix := range []string{"messages", "message-rejections"} {
		m.HandleFunc("POST /v1/requests/{id}/"+suffix, func(w http.ResponseWriter, r *http.Request) {
			var in execution.ConversationInput
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
			d.DisallowUnknownFields()
			if d.Decode(&in) != nil || in.RequestID != r.PathValue("id") {
				write(w, 400, map[string]string{"error": "InvalidInput"})
				return
			}
			var extra any
			if d.Decode(&extra) != io.EOF {
				write(w, 400, map[string]string{"error": "InvalidInput"})
				return
			}
			var v execution.ConversationReceipt
			var e error
			if r.Pattern == "POST /v1/requests/{id}/message-rejections" {
				v, e = s.RejectMessage(r.Context(), in)
			} else {
				v, e = s.QueueMessage(r.Context(), in)
			}
			if e == execution.ErrInvalid {
				write(w, 409, map[string]string{"error": "Conflict"})
				return
			}
			result(w, v, e)
		})
	}
}
