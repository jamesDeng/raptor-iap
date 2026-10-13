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
