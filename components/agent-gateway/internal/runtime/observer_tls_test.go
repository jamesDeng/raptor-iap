package runtime

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObserverKubeFactoryRequiresVerifiedClusterCA(t *testing.T) {
	token := func() (string, error) { return "issuer-token", nil }
	if _, err := NewObserverKubeIssuer("https://10.70.0.1", "raptor-test", "rdev-test-client-observer", []byte("invalid-ca"), token); err != ErrConfiguration {
		t.Fatal("invalid CA accepted", err)
	}
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	issuer, err := NewObserverKubeIssuer("https://10.70.0.1", "raptor-test", "rdev-test-client-observer", ca, token)
	if err != nil {
		t.Fatal(err)
	}
	transport := issuer.Client.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.RootCAs == nil || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatal("cluster TLS not verified")
	}
	if transport.Proxy != nil {
		t.Fatal("cluster credential request may use ambient proxy")
	}
}
