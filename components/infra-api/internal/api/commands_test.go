package api

import (
	"context"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

// Removing strict decoding or authorization would allow a mutation here.
func TestCommandsRejectInvalidInputBeforeSubmission(t *testing.T) {
	validBody := `{"requestId":"request","envCode":"dev","appCode":"app","clusterId":"cluster","namespace":"ns","name":"app","uid":"uid"}`
	for _, body := range []string{strings.Replace(validBody, `"uid":"uid"`, `"uid":"uid","uid":"other"`, 1), strings.Replace(validBody, `"uid":"uid"`, `"uid":"uid","extra":true`, 1), validBody + ` {}`, strings.Replace(validBody, `"uid":"uid"`, `"uid":2`, 1)} {
		cmd := &commandFixture{}
		h, err := NewWithCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", cmd, allowCommands{})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/v1/deployment-restart", strings.NewReader(body))
		r.Header.Set("X-Infra-Authorization", valid())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || cmd.calls != 0 {
			t.Fatalf("invalid body submitted: %d %s", w.Code, w.Body.String())
		}
	}
	for _, authorizer := range []Authorizer{nil, denyCommands{}} {
		cmd := &commandFixture{}
		h, err := NewWithCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", cmd, authorizer)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/v1/deployment-restart", strings.NewReader(validBody))
		r.Header.Set("X-Infra-Authorization", valid())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || cmd.calls != 0 {
			t.Fatalf("unverified request submitted: %d", w.Code)
		}
	}
}

type commandFixture struct{ calls int }

func (f *commandFixture) Restart(context.Context, domain.Environment, domain.RestartCommand) (domain.RestartReceipt, error) {
	f.calls++
	return domain.RestartReceipt{Accepted: true}, nil
}
func (f *commandFixture) Scale(context.Context, domain.Environment, domain.ScaleCommand) (domain.ScaleReceipt, error) {
	f.calls++
	return domain.ScaleReceipt{Outcome: "accepted"}, nil
}
func (f *commandFixture) SetProtection(context.Context, domain.Environment, domain.ProtectionCommand) (domain.NodeReceipt, error) {
	f.calls++
	return domain.NodeReceipt{Outcome: "accepted"}, nil
}
func (f *commandFixture) Deregister(context.Context, domain.Environment, domain.DeregisterCommand) (domain.NodeReceipt, error) {
	f.calls++
	return domain.NodeReceipt{Outcome: "accepted"}, nil
}

type allowCommands struct{}

func (allowCommands) Authorize(context.Context, string, string, any) error { return nil }

type denyCommands struct{}

func (denyCommands) Authorize(context.Context, string, string, any) error { return domain.ErrScope }

func TestCommandHTTPDispatchAndRequiredZeroValues(t *testing.T) {
	cases := []struct{ path, body string }{
		{"/v1/deployment-restart", `{"requestId":"r","envCode":"dev","appCode":"app","clusterId":"c","namespace":"n","name":"a","uid":"u"}`},
		{"/v1/db-proxy-scale", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","groupId":"g","desiredCapacity":0}`},
		{"/v1/db-proxy-node-protection", `{"requestId":"r","envCode":"dev","proxyCode":"p","groupId":"g","instanceIds":["i"],"protected":false}`},
		{"/v1/db-proxy-node-deregistration", `{"requestId":"r","envCode":"dev","proxyCode":"p","groupId":"g","serverGroupId":"s","instanceIds":["i"]}`},
	}
	for _, tc := range cases {
		cmd := &commandFixture{}
		op := operations{scopes: map[string]domain.Environment{"dev": {Code: "dev"}}, commander: cmd, authorizer: allowCommands{}, principal: "user"}
		direct, e := op.command(context.Background(), tc.path, []byte(tc.body))
		if e != nil || direct == nil || cmd.calls != 1 {
			t.Fatal(direct, e)
		}
		h, e := NewWithCommands(fakeReader{}, op.scopes, "user", "secret", "X-Infra-Authorization", cmd, allowCommands{})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("X-Infra-Authorization", valid())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || cmd.calls != 2 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestConfiguredAuthorizerAlsoRestrictsReadTools(t *testing.T) {
	f := &observedReader{}
	h, e := NewWithCommands(f, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", nil, denyCommands{})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/v1/cloud/identity?envCode=dev", nil)
	r.Header.Set("X-Infra-Authorization", valid())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || f.calls.Load() != 0 {
		t.Fatalf("request read scope bypass: %d calls=%d", w.Code, f.calls.Load())
	}
}

func (allowCommands) ExposeCommands() bool { return true }
