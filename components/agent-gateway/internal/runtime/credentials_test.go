package runtime

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fc "github.com/alibabacloud-go/fcsandbox-20260509/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/aliyun/credentials-go/credentials"
)

// A missing shared lock would allow concurrent calls into the upstream mutable cache.
type rotatingCredential struct {
	credentials.Credential
	generation atomic.Int64
	active     atomic.Int32
	overlap    atomic.Bool
	fail       atomic.Bool
}

func (r *rotatingCredential) GetCredential() (*credentials.CredentialModel, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	time.Sleep(time.Microsecond)
	if r.fail.Load() {
		return nil, errors.New("private-sts-token")
	}
	g := "A"
	if r.generation.Load() > 0 {
		g = "B"
	}
	return &credentials.CredentialModel{AccessKeyId: dara.String("id" + g), AccessKeySecret: dara.String("secret" + g), SecurityToken: dara.String("token" + g), Type: dara.String("sts")}, nil
}
func TestSharedCredentialsSerializeAndRotate(t *testing.T) {
	raw := &rotatingCredential{}
	shared := &lockedCredential{source: raw}
	ossProvider := ossCredentialProvider{source: shared}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := ossProvider.GetCredentialsE()
			if e != nil || c.GetAccessKeyID() != "idA" || c.GetSecurityToken() != "tokenA" {
				t.Error("invalid atomic credentials")
			}
		}()
	}
	wg.Wait()
	if raw.overlap.Load() {
		t.Fatal("concurrent upstream cache access")
	}
	raw.generation.Store(1)
	c, e := ossProvider.GetCredentialsE()
	if e != nil || c.GetAccessKeyID() != "idB" || c.GetAccessKeySecret() != "secretB" || c.GetSecurityToken() != "tokenB" {
		t.Fatal("rotation not observed")
	}
	raw.fail.Store(true)
	if _, e = ossProvider.GetCredentialsE(); e == nil || strings.Contains(e.Error(), "private-sts-token") {
		t.Fatal("refresh failure not sanitized")
	}
}

type signingHTTP struct {
	tokens []string
	fail   bool
}

func (s *signingHTTP) Call(r *http.Request, _ *http.Transport) (*http.Response, error) {
	token := r.Header.Get("x-acs-security-token")
	for k, v := range r.Header {
		if strings.EqualFold(k, "x-acs-security-token") && len(v) > 0 {
			token = v[0]
		}
	}
	s.tokens = append(s.tokens, token)
	if s.fail {
		return nil, errors.New("unknown submission outcome")
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"apiKeys":[],"total":0}`))}, nil
}
func TestFCUsesRotatedCredentialsWithoutReplay(t *testing.T) {
	raw := &rotatingCredential{}
	shared := &lockedCredential{source: raw}
	m, _, err := newCloudClients(testCloudConfig(), shared)
	if err != nil {
		t.Fatal(err)
	}
	h := &signingHTTP{}
	m.Client.HttpClient = h
	for i := 0; i < 2; i++ {
		raw.generation.Store(int64(i))
		_, err = m.Client.ListApiKeysWithOptions(&fc.ListApiKeysRequest{TeamID: dara.String("team")}, nil, sdkOptions())
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(h.tokens) != 2 || h.tokens[0] != "tokenA" || h.tokens[1] != "tokenB" {
		t.Fatalf("rotation signing failed: %v", h.tokens)
	}
	raw.fail.Store(true)
	_, err = m.Client.ListApiKeysWithOptions(&fc.ListApiKeysRequest{TeamID: dara.String("team")}, nil, sdkOptions())
	if err == nil || len(h.tokens) != 2 {
		t.Fatal("refresh failure sent cloud request")
	}
	raw.fail.Store(false)
	h.fail = true
	_, err = m.Client.ListApiKeysWithOptions(&fc.ListApiKeysRequest{TeamID: dara.String("team")}, nil, sdkOptions())
	if err == nil || len(h.tokens) != 3 {
		t.Fatal("cloud request replayed")
	}
}
func testCloudConfig() LiveConfig {
	return LiveConfig{InfraUsername: "infra", InfraPassword: "test-password", AccountID: "1360282071200743", Region: "ap-southeast-1", TeamID: "team", TemplateID: "template", VolumeID: "volume", VolumeName: "auth", Bucket: "bucket", BucketPrefix: "auth", ExecutionRoleARN: "acs:ram::1360282071200743:role/runtime", HarnessDir: "/opt/harness", RaptorMcpURL: "https://raptor.example/mcp", InfraMcpURL: "https://infra.example/mcp"}
}

func TestOIDCProjectedTokenRotationAndFailure(t *testing.T) {
	var tokens []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("Action") != "AssumeRoleWithOIDC" {
			t.Error("wrong STS action")
		}
		r.ParseForm()
		tokens = append(tokens, r.Form.Get("OIDCToken"))
		if len(tokens) == 3 {
			w.WriteHeader(503)
			io.WriteString(w, "private-sts-token")
			return
		}
		g := "A"
		if len(tokens) == 2 {
			g = "B"
		}
		json.NewEncoder(w).Encode(map[string]any{"Credentials": map[string]any{"AccessKeyId": "id" + g, "AccessKeySecret": "secret" + g, "SecurityToken": "token" + g, "Expiration": time.Now().Add(120 * time.Second).UTC().Format(time.RFC3339)}})
	}))
	defer srv.Close()
	old := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = old }()
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("projected-A"), 0600)
	raw, err := credentials.NewCredential(&credentials.Config{Type: dara.String("oidc_role_arn"), RoleArn: dara.String("acs:ram::1360282071200743:role/gateway"), OIDCProviderArn: dara.String("acs:ram::1360282071200743:oidc-provider/ack"), OIDCTokenFilePath: dara.String(token), STSEndpoint: dara.String(strings.TrimPrefix(srv.URL, "https://"))})
	if err != nil {
		t.Fatal(err)
	}
	shared := &lockedCredential{source: raw}
	a, err := shared.GetCredential()
	if err != nil || dara.StringValue(a.SecurityToken) != "tokenA" {
		t.Fatal("initial exchange failed")
	}
	replacement := token + ".next"
	os.WriteFile(replacement, []byte("projected-B"), 0600)
	os.Rename(replacement, token)
	b, err := shared.GetCredential()
	if err != nil || dara.StringValue(b.SecurityToken) != "tokenB" {
		t.Fatal("refresh failed")
	}
	if len(tokens) != 2 || tokens[0] != "projected-A" || tokens[1] != "projected-B" {
		t.Fatalf("projected rotation not read: %v", tokens)
	}
	_, err = shared.GetCredential()
	if err == nil || strings.Contains(err.Error(), "private-sts-token") {
		t.Fatal("failed exchange not sanitized")
	}
}

type ossSigningTransport struct{ tokens []string }

func (s *ossSigningTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.tokens = append(s.tokens, r.Header.Get("x-oss-security-token"))
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"1"}}, Body: io.NopCloser(strings.NewReader("x")), ContentLength: 1}, nil
}
func TestOSSSigningRotationAndFailClosed(t *testing.T) {
	raw := &rotatingCredential{}
	shared := &lockedCredential{source: raw}
	_, v, err := newCloudClients(testCloudConfig(), shared)
	if err != nil {
		t.Fatal(err)
	}
	h := &ossSigningTransport{}
	old := http.DefaultTransport
	http.DefaultTransport = h
	defer func() { http.DefaultTransport = old }()
	objects := v.Objects.(OSSObjects)
	for i := 0; i < 2; i++ {
		raw.generation.Store(int64(i))
		body, e := objects.Bucket.GetObject("auth/a")
		if e != nil {
			t.Fatal(e)
		}
		body.Close()
	}
	if len(h.tokens) != 2 || h.tokens[0] != "tokenA" || h.tokens[1] != "tokenB" {
		t.Fatalf("OSS rotation signing failed: %v", h.tokens)
	}
	raw.fail.Store(true)
	_, err = objects.Bucket.GetObject("auth/a")
	if err == nil || len(h.tokens) != 2 {
		t.Fatal("OSS sent request after refresh failure")
	}
}
func TestCredentialModeRejectsMissingMixedAndUnknown(t *testing.T) {
	for _, mode := range []string{"unknown", "rrsa"} {
		t.Setenv("GATEWAY_CONTROLLER_CREDENTIAL_MODE", mode)
		t.Setenv("GATEWAY_CONTROLLER_CREDENTIAL_FILE", "")
		t.Setenv("ALIBABA_CLOUD_ROLE_ARN", "")
		if _, _, e := CloudClientsFromEnvironment(testCloudConfig()); e == nil {
			t.Fatal("invalid mode accepted")
		}
	}
	t.Setenv("GATEWAY_CONTROLLER_CREDENTIAL_FILE", "/private/static.json")
	if _, _, e := CloudClientsFromEnvironment(testCloudConfig()); e == nil {
		t.Fatal("mixed modes accepted")
	}
}

func TestMalformedOIDCResponseCannotCrashGateway(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"Credentials":{"AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token"}}`)
	}))
	defer srv.Close()
	old := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = old }()
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("projected"), 0600)
	raw, e := credentials.NewCredential(&credentials.Config{Type: dara.String("oidc_role_arn"), RoleArn: dara.String("acs:ram::1360282071200743:role/gateway"), OIDCProviderArn: dara.String("acs:ram::1360282071200743:oidc-provider/ack"), OIDCTokenFilePath: dara.String(token), STSEndpoint: dara.String(strings.TrimPrefix(srv.URL, "https://"))})
	if e != nil {
		t.Fatal(e)
	}
	shared := &lockedCredential{source: raw}
	if _, e = shared.GetCredential(); e == nil {
		t.Fatal("malformed response accepted")
	}
}
