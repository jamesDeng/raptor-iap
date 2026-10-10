package requests

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/modelproviders"
	"strings"
	"testing"
)

type selectedModel struct {
	enabled bool
	version int64
}

func (m *selectedModel) ValidateSelection(_ context.Context, p modelproviders.ProviderID, id modelproviders.ModelID) (int64, error) {
	if !m.enabled || p != "codex" || id != "gpt-6-sol" {
		return 0, modelproviders.ErrInvalidModel
	}
	return m.version, nil
}

func TestQuestionSelectedModelIsBoundAndStaleSelectionRejected(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	obj, err := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "Model Bound"})
	if err != nil {
		t.Fatal(err)
	}
	p := &selectedModel{enabled: true, version: 3}
	s.ModelPolicy = p
	in := questionInput(t, "health?", "gpt-6-sol")
	in.ProviderID = "codex"
	in.Object.Code = obj.Code
	in.EnvCode = "adev"
	r, err := s.Create(ctx, u, "model-bound", in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Definition.ProviderID != "codex" || r.Definition.Model != "gpt-6-sol" || r.Definition.ConnectionVersion != 3 {
		t.Fatalf("binding lost: %+v", r.Definition)
	}
	in.ConnectionVersion = 99
	if _, err = s.Create(ctx, u, "forged", in); err == nil {
		t.Fatal("browser supplied connection version accepted")
	}
	in.ConnectionVersion = 0
	p.enabled = false
	if _, err = s.Create(ctx, u, "stale", in); err == nil {
		t.Fatal("disabled model accepted")
	}
	again, err := s.Create(ctx, u, "model-bound", in)
	if err != nil || again.ID != r.ID {
		t.Fatalf("idempotent retry changed: %v", err)
	}
}

func questionInput(t *testing.T, question, model string) domain.RequestInput {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "agent", "object": map[string]string{"kind": "application", "code": "existing"}, "envCode": "rdev.ali", "model": model, "operations": []any{map[string]any{"name": "application.question", "parameters": map[string]string{"question": question}}}})
	var in domain.RequestInput
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatal(err)
	}
	return in
}
func TestQuestionUnicodeAndModelValidation(t *testing.T) {
	s := &Service{}
	for _, question := range []string{"Is it healthy?", strings.Repeat("😀", 2000)} {
		if err := s.Validate(questionInput(t, question, "gpt-5.6-luna")); err != nil {
			t.Fatalf("valid question rejected: %v", err)
		}
	}
	for _, tc := range []struct{ question, model string }{{" ", "gpt-5.6-luna"}, {strings.Repeat("a", 2001), "gpt-5.6-luna"}, {"health?", "other"}, {"health?", ""}} {
		if err := s.Validate(questionInput(t, tc.question, tc.model)); err == nil {
			t.Fatal("invalid question accepted")
		}
	}
	in := questionInput(t, "health?", "gpt-5.6-luna")
	in.Operations = append(in.Operations, in.Operations[0])
	if err := s.Validate(in); err == nil {
		t.Fatal("multiple question operations accepted")
	}
	in = questionInput(t, "health?", "gpt-5.6-luna")
	in.Operations[0].Parameters["question"] = string([]byte{0xff})
	if err := s.Validate(in); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestQuestionRequiresExistingSelectionAndPinsIdentity(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	obj, err := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "Gateway"})
	if err != nil {
		t.Fatal(err)
	}
	s.ResolveSkills = func(context.Context, domain.SkillsVersion) (domain.SkillsVersion, error) {
		return domain.SkillsVersion{Tag: "release", CommitSHA: strings.Repeat("a", 40)}, nil
	}
	in := questionInput(t, "health?", "gpt-5.6-luna")
	in.Object.Code = obj.Code
	in.EnvCode = "adev"
	r, err := s.Create(ctx, u, "question", in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Create(ctx, u, "question", in)
	if err != nil || again.ID != r.ID {
		t.Fatal("retry identity changed", err)
	}
	if r.Definition.Skills.CommitSHA != strings.Repeat("a", 40) {
		t.Fatal("skills not pinned")
	}
	b, _ := json.Marshal(r.Definition)
	var wire map[string]any
	json.Unmarshal(b, &wire)
	if wire["model"] != "gpt-5.6-luna" {
		t.Fatal("model lost")
	}
	in.Object.Code = "missing"
	if _, err = s.Create(ctx, u, "missing-object", in); err == nil {
		t.Fatal("missing object accepted")
	}
	in.Object.Code = obj.Code
	in.EnvCode = "missing"
	if _, err = s.Create(ctx, u, "missing-env", in); err == nil {
		t.Fatal("missing environment accepted")
	}
}
func TestQuestionCancelAwaitsGatewayCleanup(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	obj, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "Q"})
	if e != nil {
		t.Fatal(e)
	}
	in := questionInput(t, "health?", "gpt-5.6-luna")
	in.Object.Code = obj.Code
	in.EnvCode = "adev"
	r, e := s.Create(ctx, u, "cancel-question", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, r.ID, "continue", "resume"); e == nil {
		t.Fatal("unsupported live continuation accepted")
	}
	if e = s.Action(ctx, r.ID, "cancel", ""); e != nil {
		t.Fatal(e)
	}
	saved, e := s.Get(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if saved.Status == "cancelled" {
		t.Fatal("cancel marked terminal before Gateway cleanup")
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE topic='signal' AND entity_id=$1", r.ID).Scan(&count)
	if count != 1 {
		t.Fatal("cancel signal missing")
	}
}

func TestQuestionLegacyAliasPinsCanonicalCode(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	obj, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "Migrated"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO raptor.object_code_aliases(code,object_id,kind,canonical_code) VALUES('old-app',$1,'application',$2)", obj.ID, obj.Code); e != nil {
		t.Fatal(e)
	}
	s.ResolveSkills = func(context.Context, domain.SkillsVersion) (domain.SkillsVersion, error) {
		return domain.SkillsVersion{Tag: "release", CommitSHA: strings.Repeat("a", 40)}, nil
	}
	in := questionInput(t, "health?", "gpt-5.6-luna")
	in.Object.Code = "old-app"
	in.EnvCode = "adev"
	r, e := s.Create(ctx, u, "alias-request", in)
	if e != nil {
		t.Fatal(e)
	}
	if r.Definition.Object.Code != obj.Code {
		t.Fatalf("new request pinned legacy code %q instead of %q", r.Definition.Object.Code, obj.Code)
	}
	again, e := s.Create(ctx, u, "alias-request", in)
	if e != nil || again.ID != r.ID {
		t.Fatalf("alias retry lost idempotency: %v", e)
	}
}
