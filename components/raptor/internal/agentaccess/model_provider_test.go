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
