package runtime

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsBundleUsesVerifiedGitBlobs(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal(e, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	git("config", "user.name", "fixture")
	git("config", "user.email", "fixture@example.invalid")
	os.MkdirAll(filepath.Join(root, "agent-skills/pgcat-replacement"), 0700)
	file := filepath.Join(root, "agent-skills/pgcat-replacement/SKILL.md")
	os.WriteFile(file, []byte("pinned content"), 0600)
	git("add", ".")
	git("commit", "-m", "fixture")
	sha := git("rev-parse", "HEAD")
	git("tag", "skills-v1.0.0")
	os.WriteFile(file, []byte("dirty content"), 0600)
	raw, e := BuildSkillsBundle(context.Background(), root, execution.SkillsVersion{Tag: "skills-v1.0.0", CommitSHA: sha})
	if e != nil {
		t.Fatal(e)
	}
	var bundle SkillsBundle
	if json.Unmarshal(raw, &bundle) != nil || len(bundle.Files) != 1 || string(bundle.Files[0].Data) != "pinned content" {
		t.Fatal("working tree used")
	}
	if _, e = BuildSkillsBundle(context.Background(), root, execution.SkillsVersion{Tag: "skills-v1.0.0", CommitSHA: strings.Repeat("a", 40)}); e == nil {
		t.Fatal("wrong SHA accepted")
	}
}

func TestLegacyReplacementContractStopsBeforeRuntimeSideEffects(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		out, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatal(e, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	git("config", "user.name", "fixture")
	git("config", "user.email", "fixture@example.invalid")
	os.MkdirAll(filepath.Join(root, "agent-skills/pgcat-replacement"), 0700)
	os.WriteFile(filepath.Join(root, "agent-skills/pgcat-replacement/SKILL.md"), []byte("replacement-contract: direct-v2\n"), 0600)
	git("add", ".")
	git("commit", "-m", "fixture")
	sha := git("rev-parse", "HEAD")
	git("tag", "skills-v99.0.0")
	// Dirty compatible text must not override the selected immutable Git blob.
	os.WriteFile(filepath.Join(root, "agent-skills/pgcat-replacement/SKILL.md"), []byte("replacement-contract: resource-v1\n"), 0600)
	observed := 0
	n := NativeLive{Config: LiveConfig{SkillsRepository: root}, Observe: func(context.Context, execution.LiveStart) (ObservationMaterial, error) {
		observed++
		return ObservationMaterial{}, nil
	}}
	in := execution.LiveStart{Binding: execution.AttemptBinding{Operation: "db-proxy.replace-nodes", ExecutionMode: "replacement", SkillsCommit: sha}, Skills: execution.SkillsVersion{Tag: "skills-v99.0.0", CommitSHA: sha}, Access: execution.AgentAccess{ReplacementScope: &execution.ReplacementScope{}}}
	_, err = n.Start(context.Background(), in)
	if err == nil || err.Error() != "ResourceContractMigrationRequired" {
		t.Fatalf("legacy contract was not an explicit early gate: %v", err)
	}
	a := AXLive{NativeLive: n}
	_, err = a.Start(context.Background(), in)
	if err == nil || err.Error() != "ResourceContractMigrationRequired" {
		t.Fatalf("AX legacy contract gate: %v", err)
	}
	if observed != 0 || len(n.runs) != 0 {
		t.Fatal("legacy contract reached observation/remote runtime")
	}
}
