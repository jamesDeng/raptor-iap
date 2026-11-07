package modelproviders

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectStartBusyHasRetryableSanitizedResponse(t *testing.T) {
	w := httptest.NewRecorder()
	writeConnectStartError(w, ErrBusy)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "ProviderAuthorizationBusy") {
		t.Fatalf("busy response: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	writeConnectStartError(w, errors.New("private provider token response"))
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("provider response leaked: %d %s", w.Code, w.Body.String())
	}
}
