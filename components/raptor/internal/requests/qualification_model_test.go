package requests

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/modelproviders"
	"testing"
)

type qualificationModel struct{ enabled bool }

func (m *qualificationModel) ValidateSelection(_ context.Context, p modelproviders.ProviderID, id modelproviders.ModelID) (int64, error) {
	if !m.enabled || p != "codex" || id != "gpt-6-luna" {
		return 0, modelproviders.ErrInvalidModel
	}
	return 3, nil
}
func TestQualificationPinsEnabledModelAndConnection(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	o, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "db-proxy", Name: "Qual model"})
	if e != nil {
		t.Fatal(e)
	}
	p := &qualificationModel{enabled: true}
	s.ModelPolicy = p
	in := domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: o.Code}, EnvCode: "adev", Model: "gpt-6-luna", ProviderID: "codex", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "qualification"}}}}
	r, e := s.Create(ctx, u, "qual-model", in)
	if e != nil || r.Definition.ConnectionVersion != 3 {
		t.Fatal("model connection not bound", e, r.Definition.ConnectionVersion)
	}
	p.enabled = false
	if _, e = s.Create(ctx, u, "qual-disabled", in); e == nil {
		t.Fatal("disabled model accepted")
	}
}

func TestReplacementPinsEnabledGPT6CodexModel(t *testing.T) {
	s := &Service{ModelPolicy: &qualificationModel{enabled: true}}
	in := domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: "DP00001"}, EnvCode: "rdev.ali", ProviderID: "codex", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "replacement"}}}}
	for _, model := range []string{"gpt-6-luna", "gpt-6.1-sol"} {
		in.Model = model
		if err := s.Validate(in); err != nil {
			t.Fatalf("connected %s replacement rejected: %v", model, err)
		}
	}
	in.ProviderID = ""
	if err := s.Validate(in); err == nil {
		t.Fatal("replacement accepted without a connected provider")
	}
	in.ProviderID = "codex"
	in.Model = "gpt-5.6-luna"
	if err := s.Validate(in); err == nil {
		t.Fatal("new replacement accepted legacy GPT-5.6 model")
	}
	s.ModelPolicy = nil
	if err := s.Validate(in); err == nil {
		t.Fatal("new replacement accepted legacy model without a connection")
	}
	delete(in.Operations[0].Parameters, "mode")
	if err := s.Validate(in); err == nil {
		t.Fatal("default replacement mode bypassed model admission")
	}
}

func TestReplacementCreateRejectsLegacyAndDisabledModels(t *testing.T) {
	s, _, user := setup(t)
	ctx := context.Background()
	obj, err := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "db-proxy", Name: "Replacement model gate"})
	if err != nil {
		t.Fatal(err)
	}
	in := domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: obj.Code}, EnvCode: "adev", ProviderID: "codex", Model: "gpt-5.6-luna", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "replacement"}}}}
	if _, err = s.Create(ctx, user, "legacy-unconnected", in); err == nil {
		t.Fatal("new legacy replacement was created without a model connection")
	}
	in.Model = "gpt-6-luna"
	if _, err = s.Create(ctx, user, "gpt6-unconnected", in); err == nil {
		t.Fatal("new GPT-6 replacement was created without a model connection")
	}
	policy := &qualificationModel{enabled: false}
	s.ModelPolicy = policy
	if _, err = s.Create(ctx, user, "gpt6-disabled", in); err == nil {
		t.Fatal("new replacement was created with a disabled model")
	}
	policy.enabled = true
	created, err := s.Create(ctx, user, "gpt6-connected", in)
	if err != nil || created.Definition.ConnectionVersion != 3 {
		t.Fatalf("enabled GPT-6 replacement was not bound: %v", err)
	}
}
