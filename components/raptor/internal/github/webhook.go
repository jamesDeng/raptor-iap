package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"io"
	"net/http"
	"strings"
)

func (s *Service) Handler(secret string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 262144))
		if e != nil {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		sig, e := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256="))
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		if secret == "" || e != nil || !hmac.Equal(mac.Sum(nil), sig) {
			httpx.Error(w, 401, "Unauthenticated")
			return
		}
		e = s.HandleDelivery(r.Context(), r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-GitHub-Event"), body)
		httpx.Result(w, 202, map[string]bool{"accepted": e == nil}, e)
	})
}
