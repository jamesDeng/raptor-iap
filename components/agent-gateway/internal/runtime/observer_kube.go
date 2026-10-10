package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type ObserverKubeCredential struct {
	Token     string
	ExpiresAt time.Time
}

func (ObserverKubeCredential) String() string     { return "[observer Kubernetes credential]" }
func (c ObserverKubeCredential) GoString() string { return c.String() }

type ObserverKubeIssuer struct {
	Server, Namespace, ServiceAccount string
	Token                             func() (string, error)
	Client                            *http.Client
}

func (i ObserverKubeIssuer) Issue(ctx context.Context) (ObserverKubeCredential, error) {
	u, err := url.Parse(i.Server)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !net.ParseIP(u.Hostname()).IsPrivate() || i.Namespace != "raptor-test" || !regexp.MustCompile(`^[a-z0-9-]{1,63}-observer$`).MatchString(i.ServiceAccount) || i.Token == nil || i.Client == nil {
		return ObserverKubeCredential{}, ErrConfiguration
	}
	if transport, ok := i.Client.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		return ObserverKubeCredential{}, ErrConfiguration
	}
	token, err := i.Token()
	if err != nil || token == "" || strings.ContainsAny(token, "\r\n") {
		return ObserverKubeCredential{}, ErrRuntimeUnavailable
	}
	endpoint := strings.TrimSuffix(i.Server, "/") + "/api/v1/namespaces/" + i.Namespace + "/serviceaccounts/" + i.ServiceAccount + "/token"
	body := []byte(`{"apiVersion":"authentication.k8s.io/v1","kind":"TokenRequest","spec":{"expirationSeconds":900}}`)
	request, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return ObserverKubeCredential{}, ErrRuntimeUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	client := *i.Client
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return ObserverKubeCredential{}, ErrRuntimeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 201 && response.StatusCode != 200 {
		return ObserverKubeCredential{}, ErrRuntimeUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return ObserverKubeCredential{}, ErrRuntimeUnavailable
	}
	var result struct {
		Status struct {
			Token               string    `json:"token"`
			ExpirationTimestamp time.Time `json:"expirationTimestamp"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Status.Token == "" || len(result.Status.Token) > 16384 || result.Status.ExpirationTimestamp.Before(time.Now().Add(120*time.Second)) || result.Status.ExpirationTimestamp.After(time.Now().Add(930*time.Second)) {
		return ObserverKubeCredential{}, ErrRuntimeUnavailable
	}
	return ObserverKubeCredential{Token: result.Status.Token, ExpiresAt: result.Status.ExpirationTimestamp}, nil
}
