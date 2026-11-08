package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/resources"
)

type inventoryTestReader struct {
	observedReader
	writes int
	out    error
}

func (r *inventoryTestReader) SyncPgcatInventory(ctx context.Context, _ domain.Environment, in domain.ProxyReadTarget, _ []string, admit func(context.Context) error) (domain.PgcatInventoryReceipt, error) {
	if err := admit(ctx); err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	r.writes++
	if in.RequestID != "r" || in.ProxyCode != "p" || in.GroupID != "g" || in.ServerGroupID != "sg" {
		return domain.PgcatInventoryReceipt{}, domain.ErrScope
	}
	return domain.PgcatInventoryReceipt{Changed: true, UID: "cfg-uid", ResourceVersion: "43", InstanceIDs: []string{"i1", "i2"}}, r.out
}

func (r *inventoryTestReader) ObservePgcatExpansion(_ context.Context, _ domain.Environment, _, activity string) (domain.PgcatExpansionObservation, error) {
	return domain.PgcatExpansionObservation{ScalingActivityID: activity, Status: "Successful", CreatedIDs: []string{"i1", "i2"}, ObservedAt: time.Now()}, nil
}

type inventoryTestGrants struct {
	claims              int
	approved            bool
	duplicate           bool
	cancelled           bool
	outcome, providerID string
	action              approval.GrantAction
}

func (g *inventoryTestGrants) ReadPgcatRequestState(_ context.Context, request, digest string) (approval.PgcatRequestState, error) {
	return approval.PgcatRequestState{
		Actions: []approval.GrantActionDetails{{RequestID: request, EnvCode: "dev", ScopeDigest: digest, ActionID: "expand",
			Capability: approval.GrantCapability{Capability: "db-proxy.scale-out", Target: approval.GrantTarget{Kind: "db-proxy", Code: "p"}},
			State:      "submitted", ProviderRequestID: "provider:asa-expand"}},
		Observations: []approval.PgcatObservation{{Kind: "hold", Subject: "fleet", Data: json.RawMessage(`{"sockets":{"old1":{},"old2":{}}}`)}},
	}, nil
}

func (g *inventoryTestGrants) ClaimGrant(_ context.Context, action approval.GrantAction, proof resources.Proof) (approval.GrantReceipt, error) {
	g.claims++
	g.action = action
	if !g.approved || action.ScopeDigest != proof.ResolutionDigest {
		return approval.GrantReceipt{}, errors.New("not approved")
	}
	return approval.GrantReceipt{Claimed: !g.duplicate}, nil
}

func (g *inventoryTestGrants) DispatchGrant(_ context.Context, _ approval.GrantAction) error {
	if g.cancelled {
		return errors.New("request cancelled")
	}
	return nil
}

func (g *inventoryTestGrants) RecordGrant(_ context.Context, _ approval.GrantAction, outcome, providerID string) error {
	g.outcome, g.providerID = outcome, providerID
	return nil
}

func TestResourceInventoryRequiresScopedGrantBeforeWriting(t *testing.T) {
	const grantID = "33333333-3333-4333-8333-333333333333"
	const input = `{"requestId":"r","envCode":"dev","proxyCode":"p","actionId":"sync-four"}`
	for _, test := range []struct {
		name                           string
		protocol, token, grant         string
		approved, duplicate, cancelled bool
		error                          error
		status, writes, claims         int
		outcome                        string
	}{
		{name: "no approval reference", protocol: "request-grants-v1", token: agentCredential(), status: 403},
		{name: "legacy reference", token: agentCredential(), grant: grantID, status: 403},
		{name: "pending grant", protocol: "request-grants-v1", token: agentCredential(), grant: grantID, status: 403, claims: 1},
		{name: "submitted action replay", protocol: "request-grants-v1", token: agentCredential(), grant: grantID, approved: true, duplicate: true, status: 409, claims: 1},
		{name: "cancelled after claim", protocol: "request-grants-v1", token: agentCredential(), grant: grantID, approved: true, cancelled: true, status: 409, claims: 1, outcome: "not_submitted"},
		{name: "valid grant", protocol: "request-grants-v1", token: agentCredential(), grant: grantID, approved: true, status: 200, claims: 1, writes: 1, outcome: "submitted"},
		{name: "unknown Kubernetes submission", protocol: "request-grants-v1", token: agentCredential(), grant: grantID, approved: true, status: 503, claims: 1, writes: 1,
			error: &domain.CommandError{Code: "SubmissionUnknown"}, outcome: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity, proof := replacementResourceIdentity()
			identity.Binding.ApprovalProtocol = test.protocol
			reader := &inventoryTestReader{out: test.error}
			grants := &inventoryTestGrants{approved: test.approved, duplicate: test.duplicate, cancelled: test.cancelled}
			resolver := &resourceFixture{rows: resourceRows(), proof: proof}
			handler, err := NewResourceServer(reader, resolver, "user", "secret", "X-Infra-Authorization", &commandFixture{},
				NewServiceAuthorizer("user", "/v1/pgcat-monitoring-sync"),
				func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return identity, nil }, grants)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/pgcat-monitoring-sync", strings.NewReader(input))
			request.Header.Set("X-Infra-Authorization", test.token)
			if test.grant != "" {
				request.Header.Set(requestGrantHeader, test.grant)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, request)
			if w.Code != test.status || reader.writes != test.writes || grants.claims != test.claims || grants.outcome != test.outcome {
				t.Fatalf("status %d body %s writes %d claims %d outcome %q", w.Code, w.Body.String(), reader.writes, grants.claims, grants.outcome)
			}
			if test.status == 200 && (grants.action.Capability.Capability != "db-proxy.sync-monitoring" ||
				grants.action.Capability.Target.Code != "p" || grants.providerID != "cfg-uid:43") {
				t.Fatalf("unbound receipt: %+v %s", grants.action, grants.providerID)
			}
		})
	}
}
