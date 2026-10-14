package agentaccess

import (
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func TestQuestionAcceptsBoundCodexModel(t *testing.T) {
	def := domain.RequestInput{
		Type: "agent", ProviderID: "codex", ConnectionVersion: 2, Model: "gpt-6-sol",
		Object:     domain.ObjectRef{Kind: "application"},
		Operations: []domain.Operation{{Name: "application.question"}},
		Skills:     domain.SkillsVersion{CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	if !question(def) {
		t.Fatal("selected Codex Request was rejected before issuing attempt access")
	}
	def.ConnectionVersion = 0
	if question(def) {
		t.Fatal("unbound Codex Request was accepted")
	}
}

func TestRepositoryChangeHasSeparateSupportedBinding(t *testing.T) {
	def := domain.RequestInput{Type: "agent", Model: "gpt-5.6-luna", Object: domain.ObjectRef{Kind: "application"}, Operations: []domain.Operation{{Name: "repository.change", Parameters: map[string]any{"instruction": "Update config"}}}, Skills: domain.SkillsVersion{CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if !supported(def) || question(def) {
		t.Fatal("repository change was not admitted separately from question")
	}
	def.Operations[0].Name = "application.question"
	if !question(def) {
		t.Fatal("question binding changed")
	}
}

func TestRepositoryChangeSelectorsExcludeInfra(t *testing.T) {
	b := Binding{Operation: "repository.change", RequestID: "request", EnvCode: "rdev.ali", ObjectKind: "application", ObjectCode: "app"}
	if !RepositorySelectorsAllowed(b, "raptor", "", nil) {
		t.Fatal("repository MCP handshake denied")
	}
	if RepositorySelectorsAllowed(b, "infra", "", nil) || RepositorySelectorsAllowed(b, "raptor", "", map[string]string{"requestId": "request"}) {
		t.Fatal("repository handshake escaped its empty Raptor scope")
	}
	if !RepositorySelectorsAllowed(b, "raptor", "environment_get", map[string]string{"requestId": "request", "envCode": "rdev.ali"}) {
		t.Fatal("bound environment read denied")
	}
	if RepositorySelectorsAllowed(b, "infra", "cloud_identity_get", map[string]string{"envCode": "rdev.ali"}) {
		t.Fatal("Git operation received Infra API access")
	}
	if RepositorySelectorsAllowed(b, "raptor", "environment_get", map[string]string{"requestId": "other", "envCode": "rdev.ali"}) {
		t.Fatal("foreign request accepted")
	}
}
