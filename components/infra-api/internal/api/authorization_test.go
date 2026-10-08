package api

import (
	"context"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"testing"
)

func TestQuestionScopeRejectsForeignObjectsAndAllCommands(t *testing.T) {
	a := QuestionAuthorizer{RequestID: "r", AttemptID: "attempt", EnvCode: "dev", AppCode: "app", Verify: func(context.Context, string) (QuestionScope, error) {
		return QuestionScope{RequestID: "r", AttemptID: "attempt", EnvCode: "dev", AppCode: "app", Active: true}, nil
	}}
	for _, tc := range []struct {
		path  string
		args  any
		allow bool
	}{
		{"/v1/cloud/identity", url.Values{"envCode": {"dev"}}, true},
		{"/v1/deployments", url.Values{"envCode": {"dev"}, "kind": {"application"}, "code": {"app"}}, true},
		{"/v1/deployments", url.Values{"envCode": {"dev"}, "kind": {"database"}, "code": {"app"}}, false},
		{"/v1/deployments", url.Values{"envCode": {"dev"}, "kind": {"application"}, "code": {"foreign"}}, false},
		{"/v1/deployment-status", url.Values{"envCode": {"dev"}, "appCode": {"foreign"}}, false},
		{"/v1/deployment-restart", domain.RestartCommand{RequestID: "r", EnvCode: "dev", AppCode: "app"}, false},
	} {
		e := a.Authorize(context.Background(), "verified-principal", tc.path, tc.args)
		if (e == nil) != tc.allow {
			t.Fatalf("%s: %v", tc.path, e)
		}
	}
	a.Verify = func(context.Context, string) (QuestionScope, error) {
		return QuestionScope{RequestID: "r", AttemptID: "attempt", EnvCode: "dev", AppCode: "app", Active: false}, nil
	}
	if a.Authorize(context.Background(), "principal", "/v1/cloud/identity", url.Values{"envCode": {"dev"}}) == nil {
		t.Fatal("terminal attempt allowed")
	}
}
