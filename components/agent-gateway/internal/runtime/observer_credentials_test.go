package runtime

import (
	"context"
	"encoding/json"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestObserverSessionUsesFixedRolePolicyAndShortExpiry(t *testing.T) {
	source := &lockedCredential{source: &rotatingCredential{}}
	var captured ObserverSessionRequest
	issuer := ObserverCredentialIssuer{AccountID: "1360282071200743", Source: source, Assume: func(_ context.Context, r ObserverSessionRequest) (*ObserverCredential, error) {
		captured = r
		return &ObserverCredential{AccessKeyID: "observer-id", AccessKeySecret: "observer-secret", SecurityToken: "observer-token", ExpiresAt: time.Now().Add(15 * time.Minute)}, nil
	}}
	out, err := issuer.Issue(context.Background(), "asg-test", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	if captured.DurationSeconds != 900 || captured.RoleARN != "acs:ram::1360282071200743:role/raptor-rdev-agent-observer" {
		t.Fatal("wrong role or TTL")
	}
	var policy struct {
		Statement []struct {
			Action   []string
			Resource []string
		}
	}
	if json.Unmarshal([]byte(captured.Policy), &policy) != nil || len(policy.Statement) != 1 || len(policy.Statement[0].Action) != 2 || policy.Statement[0].Action[0] != "ess:DescribeScalingInstances" || policy.Statement[0].Action[1] != "ess:DescribeScalingGroups" || policy.Statement[0].Resource[0] != "acs:ess:ap-southeast-1:1360282071200743:scalinggroup/asg-test" {
		t.Fatal("scope mismatch")
	}
	if strings.Contains(out.String(), "observer-secret") || out.ExpiresAt.Before(time.Now().Add(2*time.Minute)) {
		t.Fatal("unsafe output")
	}
}

func TestObserverSessionRefusesUnsafeExpiryAndSanitizesFailure(t *testing.T) {
	for _, seconds := range []int{-1, 30, 1800} {
		issuer := ObserverCredentialIssuer{AccountID: "1360282071200743", Source: &lockedCredential{source: &rotatingCredential{}}, Assume: func(context.Context, ObserverSessionRequest) (*ObserverCredential, error) {
			return &ObserverCredential{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Duration(seconds) * time.Second)}, nil
		}}
		if _, err := issuer.Issue(context.Background(), "asg-test", "attempt"); err != ErrRuntimeUnavailable {
			t.Fatal("accepted unsafe expiration")
		}
	}
}

type observerCaptureHTTP struct{ t *testing.T }

func (h observerCaptureHTTP) Call(r *http.Request, _ *http.Transport) (*http.Response, error) {
	action := r.URL.Query().Get("Action")
	if action == "" {
		action = r.Header.Get("x-acs-action")
	}
	if action == "" && len(r.Header["x-acs-action"]) > 0 {
		action = r.Header["x-acs-action"][0]
	}
	if r.URL.Scheme != "https" || r.URL.Host != "sts.ap-southeast-1.aliyuncs.com" || action != "AssumeRole" {
		h.t.Fatalf("wrong STS endpoint/action: scheme=%s host=%s queryAction=%s headerAction=%s", r.URL.Scheme, r.URL.Host, r.URL.Query().Get("Action"), r.Header.Get("x-acs-action"))
	}
	q := r.URL.Query()
	if q.Get("RoleArn") != "acs:ram::1360282071200743:role/raptor-rdev-agent-observer" || q.Get("DurationSeconds") != "900" || q.Get("Policy") == "" {
		h.t.Fatal("unsafe STS request")
	}
	raw := `{"Credentials":{"AccessKeyId":"observer-id","AccessKeySecret":"observer-secret","SecurityToken":"observer-token","Expiration":"` + time.Now().Add(900*time.Second).UTC().Format(time.RFC3339) + `"}}`
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}, nil
}
func TestObserverSDKContractRetainsActualExpiration(t *testing.T) {
	client, err := openapi.NewClient(&openapi.Config{Credential: &lockedCredential{source: &rotatingCredential{}}, Endpoint: dara.String("sts.ap-southeast-1.aliyuncs.com"), Protocol: dara.String("HTTPS")})
	if err != nil {
		t.Fatal(err)
	}
	client.HttpClient = observerCaptureHTTP{t}
	result, err := observerSDKAssume(client, ObserverSessionRequest{RoleARN: "acs:ram::1360282071200743:role/raptor-rdev-agent-observer", SessionName: "raptor-attempt", Policy: `{"Version":"1"}`, DurationSeconds: 900})
	if err != nil || result.SecurityToken != "observer-token" || result.ExpiresAt.Before(time.Now().Add(850*time.Second)) {
		t.Fatal("SDK contract failed")
	}
}

func TestObserverSessionBackendPolicyUsesExactProviderResourceTypes(t *testing.T) {
	issuer := ObserverCredentialIssuer{AccountID: "1360282071200743", BackendGroupID: "sgp-test", LoadBalancerID: "nlb-test", Source: &lockedCredential{source: &rotatingCredential{}}, Assume: func(_ context.Context, r ObserverSessionRequest) (*ObserverCredential, error) {
		var policy struct {
			Statement []struct{ Action, Resource []string }
		}
		if json.Unmarshal([]byte(r.Policy), &policy) != nil || len(policy.Statement) != 3 || policy.Statement[1].Resource[0] != "acs:nlb:ap-southeast-1:1360282071200743:serverGroup/sgp-test" || policy.Statement[2].Resource[0] != "acs:nlb:ap-southeast-1:1360282071200743:loadbalancer/nlb-test" {
			t.Fatal("wrong NLB session policy")
		}
		return &ObserverCredential{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "token", ExpiresAt: time.Now().Add(900 * time.Second)}, nil
	}}
	if _, err := issuer.Issue(context.Background(), "asg-test", "attempt"); err != nil {
		t.Fatal(err)
	}
}

func TestObserverIssuerSharesReviewedControllerSource(t *testing.T) {
	config := testCloudConfig()
	management, _, err := NewCloudClients(config, ControllerCredential{AccessKeyID: "fixture", AccessKeySecret: "fixture-secret", SecurityToken: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := management.ObserverIssuer("sgp-fixture", "nlb-fixture")
	if err != nil || issuer.Source == nil || issuer.AccountID != config.AccountID || issuer.BackendGroupID != "sgp-fixture" {
		t.Fatal("reviewed source not retained", err)
	}
	if _, err := (Management{AccountID: config.AccountID}).ObserverIssuer("sgp-fixture", "nlb-fixture"); err != ErrConfiguration {
		t.Fatal("ambient credential fallback accepted")
	}
}
