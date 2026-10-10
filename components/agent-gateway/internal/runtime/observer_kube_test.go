package runtime

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type observerKubeTransport struct{ t *testing.T }

func (h observerKubeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != "https://10.70.0.1/api/v1/namespaces/raptor-test/serviceaccounts/rdev-test-client-observer/token" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer issuer-token" {
		h.t.Fatal("wrong token issuer target")
	}
	var body struct {
		Spec struct {
			ExpirationSeconds int `json:"expirationSeconds"`
		}
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Spec.ExpirationSeconds != 900 {
		h.t.Fatal("wrong token TTL")
	}
	raw := `{"status":{"token":"observer-token","expirationTimestamp":"` + time.Now().Add(900*time.Second).UTC().Format(time.RFC3339) + `"}}`
	return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}}, nil
}
func TestObserverKubeTokenIsNamedAndBounded(t *testing.T) {
	issuer := ObserverKubeIssuer{Server: "https://10.70.0.1", Namespace: "raptor-test", ServiceAccount: "rdev-test-client-observer", Token: func() (string, error) { return "issuer-token", nil }, Client: &http.Client{Transport: observerKubeTransport{t}}}
	out, err := issuer.Issue(context.Background())
	if err != nil || out.Token != "observer-token" || out.ExpiresAt.Before(time.Now().Add(800*time.Second)) {
		t.Fatal("token issuer failed")
	}
	if strings.Contains(out.String(), "observer-token") {
		t.Fatal("token exposed")
	}
}

func TestObserverKubeRefusesDisabledTLSVerification(t *testing.T) {
	issuer := ObserverKubeIssuer{Server: "https://10.70.0.1", Namespace: "raptor-test", ServiceAccount: "rdev-test-client-observer", Token: func() (string, error) { return "issuer-token", nil }, Client: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}}
	if _, err := issuer.Issue(context.Background()); err != ErrConfiguration {
		t.Fatalf("must reject insecure issuer before network: %v", err)
	}
}
