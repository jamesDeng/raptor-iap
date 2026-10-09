package requests

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMessageRelayPermanentFailureClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer server.Close()
	g := HTTPGateway{BaseURL: server.URL, Username: "service", Password: "fixture"}
	_, e := g.PutMessage(context.Background(), domain.NewID(), json.RawMessage(`{}`))
	if !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("permanent error retried forever", e)
	}
}
