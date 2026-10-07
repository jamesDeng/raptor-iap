package skills

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"strings"
	"testing"
)

func TestQuestionSkillsCannotChangeAfterCreation(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	id := domain.NewID()
	user := domain.User{ID: domain.NewID()}
	_, e := p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'q','hash','{"type":"agent","operations":[{"name":"application.question","parameters":{"question":"health?"}}]}','hash')`, id, user.ID)
	if e != nil {
		t.Fatal(e)
	}
	version := domain.SkillsVersion{Tag: "skills-v1.0.0", CommitSHA: strings.Repeat("a", 40)}
	s := &Service{Pool: p, Source: FixtureSource{Versions: []domain.SkillsVersion{version}}}
	if e = s.Change(ctx, user, id, SkillsChangeInput{Tag: version.Tag, CommitSHA: version.CommitSHA, Strategy: "interrupt"}); e == nil {
		t.Fatal("immutable question skills changed")
	}
}
