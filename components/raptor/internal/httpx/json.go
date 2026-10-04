package httpx

import (
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"io"
	"net/http"
)

func Result(w http.ResponseWriter, status int, v any, e error) {
	if e == nil {
		Write(w, status, v)
		return
	}
	code := 503
	switch {
	case errors.Is(e, domain.ErrInvalid):
		code = 400
	case errors.Is(e, domain.ErrNotFound):
		code = 404
	case errors.Is(e, domain.ErrConflict):
		code = 409
	case errors.Is(e, domain.ErrForbidden):
		code = 403
	}
	if code == 503 {
		Error(w, 503, "Unavailable")
	} else {
		Error(w, code, e.Error())
	}
}

func Write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value})
}
func Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": code, "retryable": status == 503}})
}
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		Error(w, 400, "InvalidInput")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		Error(w, 400, "InvalidInput")
		return false
	}
	return true
}
