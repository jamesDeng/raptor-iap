package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func issueWithContext(ctx context.Context, h *Server, id string, in agentaccess.IssueInput) *httptest.ResponseRecorder {
	b, _ := json.Marshal(in)
	r := httptest.NewRequest("POST", "/v1/requests/"+id+"/agent-access", bytes.NewReader(b)).WithContext(ctx)
	r.SetBasicAuth("fixture-controller", "fixture-controller-password")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Holding the request transaction while either real catalog resolution or the
// provider's catalog lookup borrows the same one-slot pool must fail this test.
func TestResourceIssuanceSmallPoolActualResolverCompletes(t *testing.T) {
	for _, kind := range []string{"first", "new-attempt", "distinct-attempts", "same-attempt"} {
		t.Run(kind, func(t *testing.T) {
			h, r, f, _ := resourceRuntimeFixture(t)
			ctx := context.Background()
			if kind == "first" {
				def := r.Definition
				def.ConnectionVersion = 0
				var err error
				r, err = h.Requests.Create(ctx, domain.User{ID: domain.NewID()}, "first-issue", def)
				if err != nil {
					t.Fatal(err)
				}
			}
			// The actual catalog path is valid before restricting its connection budget.
			if _, err := h.Catalog.InfraResourceContext(ctx, "dev", "db-proxy", r.Definition.Object.Code); err != nil {
				t.Fatal(err)
			}
			config := h.Requests.Pool.Config()
			config.MaxConns = 1
			small, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer small.Close()
			limited := New(small)
			provider := &issuanceProofFixture{resourceInfraFixture: f, catalogRead: func(ctx context.Context) error { _, err := limited.Catalog.GetEnvironment(ctx, "dev"); return err }}
			if kind == "distinct-attempts" || kind == "same-attempt" {
				provider.parties = 4
				provider.ready = make(chan struct{})
			}
			limited.Catalog.Infra = provider
			limited.RegisterService("fixture-controller", "fixture-controller-password")
			hash, _ := agentaccess.Fingerprint(r.Definition)
			expiry := time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)
			n := 1
			if kind == "distinct-attempts" || kind == "same-attempt" {
				n = 4
			}
			attempt := domain.NewID()
			attempts := make([]string, n)
			bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			results := make(chan *httptest.ResponseRecorder, n)
			for i := 0; i < n; i++ {
				attempts[i] = attempt
				if kind == "distinct-attempts" {
					attempts[i] = domain.NewID()
				}
				in := agentaccess.IssueInput{AttemptID: attempts[i], DefinitionSHA256: hash, ExpiresAt: expiry}
				go func() { results <- issueWithContext(bounded, limited, r.ID, in) }()
			}
			outer := time.NewTimer(3 * time.Second)
			defer outer.Stop()
			credentials := map[string]bool{}
			for i := 0; i < n; i++ {
				var w *httptest.ResponseRecorder
				select {
				case w = <-results:
				case <-outer.C:
					t.Fatal("issuance exceeded bounded outer wait")
				}
				if w.Code != 201 {
					t.Errorf("bounded actual resolver status=%d body=%s", w.Code, w.Body.String())
					continue
				}
				var envelope struct{ Data agentaccess.Issued }
				if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				v := envelope.Data
				if v.Binding.RequestID != r.ID || v.Binding.DefinitionSHA256 != hash || v.ReplacementScope == nil || !v.ReplacementScope.ResourceValidFor(v.Binding) || v.ReplacementScope.ListenerID != "l" || v.ReplacementScope.DatabaseResourceID != "rds" || v.ReplacementScope.Application.UID != "u" || !v.ExpiresAt.Equal(expiry) || credentials[v.Credential] {
					t.Fatal("invalid returned immutable binding/proof/credential", v)
				}
				credentials[v.Credential] = true
			}
			for _, id := range attempts {
				var count, tokens int
				if err := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.agent_attempt_access WHERE request_id=$1 AND attempt_id=$2", r.ID, id).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if err := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1 AND attempt_id=$2 AND NOT revoked", r.ID, id).Scan(&tokens); err != nil {
					t.Fatal(err)
				}
				if count != 1 || tokens != 1 {
					t.Errorf("attempt %s persisted access=%d active tokens=%d; successful responses=%d", id, count, tokens, len(credentials))
				}
			}
			if len(credentials) == n {
				live := 0
				for credential := range credentials {
					if active(t, introspection(limited, credential, "infra", "db_proxy_scale", map[string]string{"requestId": r.ID, "envCode": "dev", "proxyCode": r.Definition.Object.Code})) {
						live++
					}
				}
				want := n
				if kind == "same-attempt" {
					want = 1
				}
				if live != want {
					t.Fatalf("active returned tokens=%d want=%d", live, want)
				}
			}
		})
	}
}

// Exercise the real resolver/catalog, with only external provider proof synthetic.
// Hooks run after that proof returns, so DB changes cannot be hidden by fake locks.
func issuePhaseService(h *Server, f *resourceInfraFixture, hook func(context.Context) error) (*agentaccess.Service, *int, *int) {
	contexts, proofs := new(int), new(int)
	resolver := agentaccess.ReplacementResolver{
		Context: func(ctx context.Context, env, kind, code string) (domain.InfraResourceContext, error) {
			(*contexts)++
			return h.Catalog.InfraResourceContext(ctx, env, kind, code)
		},
		Proof: func(ctx context.Context, env, proxy string) (adapters.ResolutionProof, error) {
			(*proofs)++
			proof, err := f.ResolutionProof(ctx, env, proxy)
			if err == nil && hook != nil {
				err = hook(ctx)
			}
			return proof, err
		},
	}
	return &agentaccess.Service{Pool: h.Requests.Pool, RaptorMCPURL: "https://raptor.fixture/mcp", InfraMCPURL: "https://infra.fixture/mcp", ResolveReplacement: resolver.Resolve}, contexts, proofs
}

func TestResourceIssuanceRejectsBeforeResolution(t *testing.T) {
	for _, change := range []string{"invalid", "expired-input", "too-long", "fingerprint", "missing", "cancelled", "blocked", "waiting", "control", "cluster", "missing-cluster", "unsupported", "legacy-mode", "legacy-scope", "incomplete-scope", "inconsistent-scope", "conflicting-complete-scope", "tombstone", "revoked", "expired-attempt", "extension"} {
		t.Run(change, func(t *testing.T) {
			h, r, f, _ := resourceRuntimeFixture(t)
			ctx := context.Background()
			hash, _ := agentaccess.Fingerprint(r.Definition)
			in := agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: hash, ExpiresAt: time.Now().Add(30 * time.Second)}
			id := r.ID
			var err error
			switch change {
			case "invalid":
				in.AttemptID = "bad"
			case "expired-input":
				in.ExpiresAt = time.Now().Add(-time.Second)
			case "too-long":
				in.ExpiresAt = time.Now().Add(601 * time.Second)
			case "fingerprint":
				in.DefinitionSHA256 = "wrong"
			case "missing":
				id = domain.NewID()
			case "cancelled", "blocked", "waiting":
				_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status=$2 WHERE id=$1", r.ID, change)
			case "control":
				_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET control_state='cancelled' WHERE id=$1", r.ID)
			case "cluster":
				_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.environments SET cluster_id='foreign' WHERE code='dev'")
			case "missing-cluster":
				_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.environments SET cluster_id='',ack_cluster_id='' WHERE code='dev'")
				if err == nil {
					_, err = h.Requests.Pool.Exec(ctx, "DELETE FROM raptor.environment_settings WHERE environment_code='dev' AND key='ackClusterId'")
				}
			case "unsupported", "legacy-mode":
				def := r.Definition
				if change == "unsupported" {
					def.Operations[0].Name = "unknown"
				} else {
					def.Operations[0].Parameters = map[string]any{}
				}
				raw, _ := json.Marshal(def)
				_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET definition=$2 WHERE id=$1", r.ID, raw)
				in.DefinitionSHA256, _ = agentaccess.Fingerprint(def)
			case "legacy-scope":
				_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET replacement_scope=NULL WHERE request_id=$1", r.ID)
			case "incomplete-scope":
				_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET replacement_scope=replacement_scope-'resolutionDigest' WHERE request_id=$1", r.ID)
			case "inconsistent-scope":
				_, err = h.Requests.Pool.Exec(ctx, `UPDATE raptor.agent_attempt_access SET replacement_scope=jsonb_set(replacement_scope,'{listenerId}','"foreign"') WHERE request_id=$1`, r.ID)
			case "conflicting-complete-scope":
				var raw []byte
				err = h.Requests.Pool.QueryRow(ctx, "SELECT replacement_scope FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID).Scan(&raw)
				var scope agentaccess.ReplacementScope
				if err == nil {
					err = json.Unmarshal(raw, &scope)
				}
				if err == nil {
					scope.ListenerID = "different-valid-listener"
					scope.ResolutionDigest = scope.Identity().Digest()
					raw, _ = json.Marshal(scope)
					_, err = h.Requests.Pool.Exec(ctx, `INSERT INTO raptor.agent_attempt_access(request_id,attempt_id,binding,expires_at,replacement_scope) SELECT request_id,$2::text::uuid,jsonb_set(binding,'{attemptId}',to_jsonb($2::text)),expires_at,$3 FROM raptor.agent_attempt_access WHERE request_id=$1 LIMIT 1`, r.ID, domain.NewID(), raw)
				}
			case "tombstone":
				access := &agentaccess.Service{Pool: h.Requests.Pool}
				err = access.Revoke(ctx, r.ID, in.AttemptID)
			case "revoked", "expired-attempt", "extension":
				err = h.Requests.Pool.QueryRow(ctx, "SELECT attempt_id::text FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID).Scan(&in.AttemptID)
				if err == nil {
					if change == "revoked" {
						_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET revoked=true WHERE request_id=$1", r.ID)
					}
					if change == "expired-attempt" {
						_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET expires_at=now()-interval '1 second' WHERE request_id=$1", r.ID)
					}
					if change == "extension" {
						in.ExpiresAt = time.Now().Add(2 * time.Minute)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			var beforeAttempts, beforeTokens, beforeLive int
			if err := h.Requests.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM raptor.agent_attempt_access WHERE request_id=$1),(SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1),(SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1 AND NOT revoked)", r.ID).Scan(&beforeAttempts, &beforeTokens, &beforeLive); err != nil {
				t.Fatal(err)
			}
			svc, contexts, proofs := issuePhaseService(h, f, nil)
			if v, e := svc.Issue(ctx, id, in); e == nil || v.Credential != "" || *contexts != 0 || *proofs != 0 {
				t.Fatalf("denial reached resolution or minted credential: error=%v context=%d proof=%d", e, *contexts, *proofs)
			}
			var attempts, tokens, live int
			if err := h.Requests.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM raptor.agent_attempt_access WHERE request_id=$1),(SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1),(SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1 AND NOT revoked)", r.ID).Scan(&attempts, &tokens, &live); err != nil {
				t.Fatal(err)
			}
			if attempts != beforeAttempts || tokens != beforeTokens || live != beforeLive {
				t.Fatal("early denial changed credentials", attempts, tokens, live)
			}
		})
	}
}

func TestResourceIssuanceRejectsChangesBetweenProofAndLock(t *testing.T) {
	for _, change := range []string{"definition", "skills", "mode", "env", "provider", "connection", "cancel", "control", "cluster", "listener", "database", "uid", "incomplete", "lost-history", "tombstone", "revoke-existing", "deadline", "deadline-shortened", "deadline-extended", "expired-current-time", "provider-failure", "context-cancel", "context-deadline"} {
		t.Run(change, func(t *testing.T) {
			h, r, f, token := resourceRuntimeFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hash, _ := agentaccess.Fingerprint(r.Definition)
			in := agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: hash, ExpiresAt: time.Now().Add(30 * time.Second).UTC().Truncate(time.Microsecond)}
			if change == "revoke-existing" || (change == "deadline" || change == "deadline-shortened" || change == "deadline-extended") || change == "provider-failure" || change == "context-cancel" {
				if err := h.Requests.Pool.QueryRow(ctx, "SELECT attempt_id::text FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID).Scan(&in.AttemptID); err != nil {
					t.Fatal(err)
				}
			}
			var svc *agentaccess.Service
			var hookError error
			hook := func(ctx context.Context) (result error) {
				defer func() { hookError = result }()
				var err error
				switch change {
				case "definition", "skills", "mode", "env", "provider", "connection":
					path, value := "{model}", `"gpt-6.1-sol"`
					if change == "skills" {
						path, value = "{skills,commitSha}", `"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`
					}
					if change == "mode" {
						path, value = "{operations,0,parameters,mode}", `"qualification"`
					}
					if change == "env" {
						path, value = "{envCode}", `"foreign"`
					}
					if change == "provider" {
						path, value = "{providerId}", `"other"`
					}
					if change == "connection" {
						path, value = "{connectionVersion}", `999`
					}
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET definition=jsonb_set(definition,$2::text[],$3::jsonb) WHERE id=$1", r.ID, path, value)
				case "cancel":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status='cancelled' WHERE id=$1", r.ID)
				case "control":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET control_state='cancelled' WHERE id=$1", r.ID)
				case "cluster":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.environments SET ack_cluster_id='other',cluster_id='other' WHERE code='dev'")
				case "listener", "database", "uid":
					path := "{listenerId}"
					if change == "database" {
						path = "{databaseResourceId}"
					}
					if change == "uid" {
						path = "{application,uid}"
					}
					_, err = h.Requests.Pool.Exec(ctx, `UPDATE raptor.agent_attempt_access SET replacement_scope=jsonb_set(replacement_scope,$2::text[],'"changed"') WHERE request_id=$1`, r.ID, path)
				case "incomplete":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET replacement_scope=replacement_scope-'resolutionDigest' WHERE request_id=$1", r.ID)
				case "lost-history":
					_, err = h.Requests.Pool.Exec(ctx, "DELETE FROM raptor.agent_tokens WHERE request_id=$1", r.ID)
					if err == nil {
						_, err = h.Requests.Pool.Exec(ctx, "DELETE FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID)
					}
				case "tombstone", "revoke-existing":
					err = svc.Revoke(ctx, r.ID, in.AttemptID)
				case "deadline":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET expires_at=now()-interval '1 second' WHERE request_id=$1", r.ID)
				case "deadline-shortened", "deadline-extended":
					deadline := time.Now().Add(45 * time.Second)
					if change == "deadline-extended" {
						deadline = time.Now().Add(2 * time.Minute)
					}
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET expires_at=$2 WHERE request_id=$1", r.ID, deadline)
				case "expired-current-time":
					svc.Now = func() time.Time { return in.ExpiresAt }
				case "provider-failure":
					return domain.ErrUnavailable
				case "context-cancel":
					cancel()
				case "context-deadline":
					<-ctx.Done()
				}
				return err
			}
			var contexts, proofs *int
			svc, contexts, proofs = issuePhaseService(h, f, hook)
			timeout := 2 * time.Second
			if change == "context-deadline" {
				timeout = time.Second
			}
			bounded, stop := context.WithTimeout(ctx, timeout)
			defer stop()
			if v, e := svc.Issue(bounded, r.ID, in); e == nil || v.Credential != "" || *contexts != 1 || *proofs != 1 {
				t.Fatalf("intervening %s minted or skipped proof: %v context=%d proof=%d", change, e, *contexts, *proofs)
			}
			if hookError != nil && change != "provider-failure" {
				t.Fatal("phase change failed rather than testing admission", hookError)
			}
			var count int
			if err := h.Requests.Pool.QueryRow(context.Background(), "SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1", r.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 1
			if change == "lost-history" {
				want = 0
			}
			if count != want {
				t.Fatalf("failed issuance wrote token: %d want=%d", count, want)
			}
			var accessRows int
			if err := h.Requests.Pool.QueryRow(context.Background(), "SELECT count(*) FROM raptor.agent_attempt_access WHERE request_id=$1 AND attempt_id=$2", r.ID, in.AttemptID).Scan(&accessRows); err != nil {
				t.Fatal(err)
			}
			wantAccess := 0
			switch change {
			case "tombstone", "revoke-existing", "deadline", "deadline-shortened", "deadline-extended", "provider-failure", "context-cancel":
				wantAccess = 1
			}
			if accessRows != wantAccess {
				t.Fatalf("failed issuance changed selected attempt rows: %d want=%d", accessRows, wantAccess)
			}
			if change == "provider-failure" || change == "context-cancel" {
				if !active(t, introspection(h, token, "infra", "db_proxy_scale", map[string]string{"requestId": r.ID, "envCode": "dev", "proxyCode": r.Definition.Object.Code})) {
					t.Fatal("failed rotation revoked valid token")
				}
			}
		})
	}
}

// This external proof fake performs the same additional catalog borrow required
// by HTTPInfra, then gates all preparations before any issuance can commit.
type issuanceProofFixture struct {
	*resourceInfraFixture
	catalogRead func(context.Context) error
	parties     int32
	arrived     atomic.Int32
	ready       chan struct{}
}

func (f *issuanceProofFixture) ResolutionProof(ctx context.Context, env, proxy string) (adapters.ResolutionProof, error) {
	if f.catalogRead != nil {
		if err := f.catalogRead(ctx); err != nil {
			return adapters.ResolutionProof{}, err
		}
	}
	if f.parties > 0 {
		if f.arrived.Add(1) == f.parties {
			close(f.ready)
		}
		select {
		case <-f.ready:
		case <-ctx.Done():
			return adapters.ResolutionProof{}, ctx.Err()
		}
	}
	return f.resourceInfraFixture.ResolutionProof(ctx, env, proxy)
}

func TestResourceIssuanceCompetingRealizationsCannotBothPin(t *testing.T) {
	h, r, f, _ := resourceRuntimeFixture(t)
	ctx := context.Background()
	// Start without any frozen scope; two fresh valid but different proofs race.
	if _, err := h.Requests.Pool.Exec(ctx, "DELETE FROM raptor.agent_tokens WHERE request_id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Requests.Pool.Exec(ctx, "DELETE FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ready := make(chan struct{})
	var arrived atomic.Int32
	hash, _ := agentaccess.Fingerprint(r.Definition)
	expiry := time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)
	type result struct {
		v agentaccess.Issued
		e error
	}
	results := make(chan result, 2)
	for _, listener := range []string{"l", "competing"} {
		proof := f.proof
		proof.Identity.ListenerID = listener
		proof.ResolutionDigest = proof.Identity.Digest()
		resolver := agentaccess.ReplacementResolver{Context: h.Catalog.InfraResourceContext, Proof: func(ctx context.Context, _, _ string) (adapters.ResolutionProof, error) {
			if arrived.Add(1) == 2 {
				close(ready)
			}
			select {
			case <-ready:
				return proof, nil
			case <-ctx.Done():
				return adapters.ResolutionProof{}, ctx.Err()
			}
		}}
		svc := &agentaccess.Service{Pool: h.Requests.Pool, RaptorMCPURL: "https://raptor.fixture/mcp", InfraMCPURL: "https://infra.fixture/mcp", ResolveReplacement: resolver.Resolve}
		go func() {
			v, e := svc.Issue(bounded, r.ID, agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: hash, ExpiresAt: expiry})
			results <- result{v, e}
		}()
	}
	wins, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		var res result
		select {
		case res = <-results:
		case <-bounded.Done():
			t.Fatal("competing issuance exceeded deadline", bounded.Err())
		}
		if res.e == nil {
			wins++
			if res.v.Credential == "" {
				t.Fatal("missing winner credential")
			}
		} else if res.e == domain.ErrConflict {
			conflicts++
		} else {
			t.Fatal(res.e)
		}
	}
	var attempts, tokens int
	if err := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1 AND NOT revoked", r.ID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if wins != 1 || conflicts != 1 || attempts != 1 || tokens != 1 {
		t.Fatalf("competing pins: wins=%d conflicts=%d attempts=%d tokens=%d", wins, conflicts, attempts, tokens)
	}
}

func TestResourceIssuanceKeepsRevokedAndExpiredProofFrozen(t *testing.T) {
	for _, history := range []string{"revoked", "expired"} {
		t.Run(history, func(t *testing.T) {
			h, r, f, _ := resourceRuntimeFixture(t)
			ctx := context.Background()
			sql := "UPDATE raptor.agent_attempt_access SET revoked=true WHERE request_id=$1"
			if history == "expired" {
				sql = "UPDATE raptor.agent_attempt_access SET expires_at=now()-interval '1 second' WHERE request_id=$1"
			}
			if _, err := h.Requests.Pool.Exec(ctx, sql, r.ID); err != nil {
				t.Fatal(err)
			}
			hash, _ := agentaccess.Fingerprint(r.Definition)
			in := agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: hash, ExpiresAt: time.Now().Add(30 * time.Second)}
			original := f.proof
			f.proof.Identity.ListenerID = "retarget"
			f.proof.ResolutionDigest = f.proof.Identity.Digest()
			svc, _, _ := issuePhaseService(h, f, nil)
			if v, e := svc.Issue(ctx, r.ID, in); e != domain.ErrConflict || v.Credential != "" {
				t.Fatal("historical pin reset", v, e)
			}
			f.proof = original
			if v, e := svc.Issue(ctx, r.ID, in); e != nil || v.Credential == "" {
				t.Fatal("compatible new attempt denied by historical revocation/expiry", v, e)
			}
			var attempts, tokens int
			if err := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if err := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.agent_tokens WHERE request_id=$1", r.ID).Scan(&tokens); err != nil {
				t.Fatal(err)
			}
			if attempts != 2 || tokens != 2 {
				t.Fatalf("attempts=%d tokens=%d", attempts, tokens)
			}
		})
	}
}
