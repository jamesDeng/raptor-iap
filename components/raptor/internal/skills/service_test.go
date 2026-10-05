package skills

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"strings"
	"testing"
)

func TestHighestStableReleaseAndPin(t *testing.T) {
	ctx := context.Background()
	source := FixtureSource{Versions: []domain.SkillsVersion{{Tag: "skills-v1.9.0", CommitSHA: strings.Repeat("a", 40)}, {Tag: "skills-v1.10.0", CommitSHA: strings.Repeat("b", 40)}, {Tag: "skills-v2.0.0-beta", CommitSHA: strings.Repeat("c", 40)}}}
	s := &Service{Source: source}
	v, e := s.Resolve(ctx, "")
	if e != nil || v.Tag != "skills-v1.10.0" {
		t.Fatal("wrong default release")
	}
	v, e = s.Resolve(ctx, "skills-v1.9.0")
	if e != nil || v.CommitSHA != strings.Repeat("a", 40) {
		t.Fatal("older version unavailable")
	}
	if _, e = s.Validate(ctx, domain.SkillsVersion{Tag: v.Tag, CommitSHA: strings.Repeat("b", 40)}); e == nil {
		t.Fatal("mismatched SHA accepted")
	}
}
