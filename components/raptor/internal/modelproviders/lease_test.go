package modelproviders

import (
	"context"
	"errors"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

func TestCredentialLeaseBindsRequestAttemptAndCurrentPolicy(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := NewCredentialStore(pool, make([]byte, 32), "v1")
	raw := []byte(`{"type":"oauth","access":"private-access","refresh":"private-refresh","expires":2000000000000}`)
	if _, err := store.PutConnectedCredential(ctx, "codex", "owner", raw); err != nil {
		t.Fatal(err)
	}
	policy := PolicyStore{Pool: pool}
	if err := policy.ReplaceCatalog(ctx, "codex", []DiscoveredModel{{ModelID: "gpt-6-sol", DisplayName: "GPT 6 Sol"}}); err != nil {
		t.Fatal(err)
	}
	userID, requestID := domain.NewID(), domain.NewID()
	if _, err := pool.Exec(ctx, `INSERT INTO raptor.users(id,username,password_hash,role) VALUES($1,'lease-admin','x','admin')`, userID); err != nil {
		t.Fatal(err)
	}
	if err := policy.SetPolicy(ctx, userID, "codex", 0, []ModelID{"gpt-6-sol"}, "gpt-6-sol"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'lease','hash',$3,'schema')`, requestID, userID, `{"type":"agent","providerId":"codex","model":"gpt-6-sol","connectionVersion":1}`); err != nil {
		t.Fatal(err)
	}
	s := HTTPService{Pool: pool, Policy: policy, Credentials: store}
	binding := AttemptBinding{RequestID: requestID, AttemptID: domain.NewID(), ProviderID: "codex", ModelID: "gpt-6-sol", ConnectionVersion: 1}
	if _, err := s.LeaseCredential(ctx, binding); !errors.Is(err, ErrLeaseDenied) {
		t.Fatalf("unscheduled attempt accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO raptor.agent_attempt_access(request_id,attempt_id,binding,expires_at) VALUES($1,$2,'{}',now()+interval '20 minutes')`, requestID, binding.AttemptID); err != nil {
		t.Fatal(err)
	}
	lease, err := s.LeaseCredential(ctx, binding)
	if err != nil || string(lease.Credential) != string(raw) || lease.Generation != 1 {
		t.Fatalf("lease: %v %v", lease, err)
	}
	if _, err = s.LeaseCredential(ctx, binding); !errors.Is(err, ErrLeaseDenied) {
		t.Fatalf("replay accepted: %v", err)
	}
	if err = s.ValidateAttempt(ctx, binding); err != nil {
		t.Fatalf("active lease rejected: %v", err)
	}
	wrong := binding
	wrong.AttemptID = domain.NewID()
	wrong.ModelID = "gpt-6-luna"
	if _, err = s.LeaseCredential(ctx, wrong); !errors.Is(err, ErrLeaseDenied) {
		t.Fatalf("cross-model accepted: %v", err)
	}
	if err = policy.SetPolicy(ctx, userID, "codex", 1, []ModelID{}, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateAttempt(ctx, binding); !errors.Is(err, ErrLeaseDenied) {
		t.Fatalf("disabled active model remained valid: %v", err)
	}
	wrong = binding
	wrong.AttemptID = domain.NewID()
	if _, err = s.LeaseCredential(ctx, wrong); !errors.Is(err, ErrLeaseDenied) {
		t.Fatalf("disabled model leased: %v", err)
	}
}
