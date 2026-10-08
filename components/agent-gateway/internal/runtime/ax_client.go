package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type AXConfig struct {
	Endpoint string `json:"endpoint"`
	CAFile   string `json:"caFile"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}
type AXRequest struct {
	Action   string                   `json:"action"`
	Binding  execution.AttemptBinding `json:"binding"`
	Deadline time.Time                `json:"deadline"`
	Phase    string                   `json:"phase,omitempty"`
	Path     string                   `json:"path,omitempty"`
	Data     []byte                   `json:"data,omitempty"`
	Limit    int64                    `json:"limit,omitempty"`
}
type AXResponse struct {
	Name      string `json:"name,omitempty"`
	ProcessID string `json:"processId,omitempty"`
	Terminal  bool   `json:"terminal"`
	ExitCode  int    `json:"exitCode"`
	Data      []byte `json:"data,omitempty"`
	Absent    bool   `json:"absent"`
	Error     string `json:"error,omitempty"`
}
type AXCaller interface {
	Call(context.Context, AXRequest) (AXResponse, error)
}
type AXClient struct {
	endpoint string
	http     *http.Client
}

func validateAXEndpoint(endpoint string) error {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Port() != "8443" {
		return ErrConfiguration
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsPrivate() {
		return ErrConfiguration
	}
	return nil
}
func NewAXClient(c AXConfig) (*AXClient, error) {
	if validateAXEndpoint(c.Endpoint) != nil {
		return nil, ErrConfiguration
	}
	info, e := os.Lstat(c.KeyFile)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrConfiguration
	}
	cert, e := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if e != nil {
		return nil, ErrConfiguration
	}
	raw, e := os.ReadFile(c.CAFile)
	if e != nil {
		return nil, ErrConfiguration
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, ErrConfiguration
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}}, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, MaxIdleConns: 2}
	return &AXClient{endpoint: strings.TrimRight(c.Endpoint, "/") + "/v1/runtime", http: &http.Client{Transport: tr, Timeout: 130 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *AXClient) Call(ctx context.Context, in AXRequest) (AXResponse, error) {
	var out AXResponse
	raw, e := json.Marshal(in)
	if e != nil {
		return out, ErrConfiguration
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(raw))
	if e != nil {
		return out, ErrConfiguration
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return out, ErrRuntimeUnavailable
	}
	defer resp.Body.Close()
	d := json.NewDecoder(io.LimitReader(resp.Body, 23*1024*1024))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&out) != nil || d.Decode(&extra) != io.EOF {
		return AXResponse{}, ErrRuntimeUnavailable
	}
	if resp.StatusCode == 404 && out.Error == "ResourceAbsent" {
		return out, ErrFileNotFound
	}
	if resp.StatusCode != 200 || out.Error != "" {
		return AXResponse{}, ErrRuntimeUnavailable
	}
	return out, nil
}
