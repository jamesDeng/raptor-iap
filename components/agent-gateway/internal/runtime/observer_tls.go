package runtime

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// NewObserverKubeIssuer uses only the configured cluster CA and never ambient
// proxies. The issuer token stays in Gateway; Issue returns a named observer token.
func NewObserverKubeIssuer(server, namespace, serviceAccount string, ca []byte, token func() (string, error)) (ObserverKubeIssuer, error) {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !net.ParseIP(u.Hostname()).IsPrivate() || namespace != "raptor-test" || !regexp.MustCompile(`^[a-z0-9-]{1,63}-observer$`).MatchString(serviceAccount) || token == nil || len(ca) == 0 || len(ca) > 16384 {
		return ObserverKubeIssuer{}, ErrConfiguration
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return ObserverKubeIssuer{}, ErrConfiguration
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second}
	return ObserverKubeIssuer{Server: server, Namespace: namespace, ServiceAccount: serviceAccount, Token: token, Client: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
