package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type SkillFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"data"`
}
type SkillsBundle struct {
	Version   int         `json:"version"`
	Tag       string      `json:"tag"`
	CommitSHA string      `json:"commitSHA"`
	Files     []SkillFile `json:"files"`
}
type boundedGitBuffer struct{ bytes.Buffer }

func (b *boundedGitBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*1024*1024 {
		return 0, ErrConfiguration
	}
	return b.Buffer.Write(p)
}
func BuildSkillsBundle(ctx context.Context, repository string, version execution.SkillsVersion) ([]byte, error) {
	if repository == "" || !regexp.MustCompile(`^skills-v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version.Tag) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(version.CommitSHA) {
		return nil, ErrConfiguration
	}
	repo, e := filepath.Abs(repository)
	if e != nil {
		return nil, ErrConfiguration
	}
	for p := repo; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrConfiguration
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	git := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
		var out boundedGitBuffer
		cmd.Stdout = &out
		cmd.Stderr = &boundedGitBuffer{}
		if cmd.Run() != nil {
			return nil, ErrConfiguration
		}
		return out.Bytes(), nil
	}
	tag, e := git("rev-parse", "--verify", "refs/tags/"+version.Tag+"^{commit}")
	if e != nil || strings.TrimSpace(string(tag)) != version.CommitSHA {
		return nil, ErrConfiguration
	}
	tree, e := git("ls-tree", "-rz", "--full-tree", version.CommitSHA, "--", "agent-skills")
	if e != nil {
		return nil, e
	}
	bundle := SkillsBundle{Version: 1, Tag: version.Tag, CommitSHA: version.CommitSHA}
	total := 0
	for _, entry := range bytes.Split(tree, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		m := regexp.MustCompile(`^100644 blob ([a-f0-9]{40})\t(agent-skills/[-a-zA-Z0-9_./]+\.(md|txt|json))$`).FindStringSubmatch(string(entry))
		if m == nil {
			return nil, ErrConfiguration
		}
		name := strings.TrimPrefix(m[2], "agent-skills/")
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." {
				return nil, ErrConfiguration
			}
		}
		data, e := git("cat-file", "blob", m[1])
		if e != nil {
			return nil, e
		}
		total += len(data)
		if total > 1024*1024 || len(bundle.Files) >= 256 {
			return nil, ErrConfiguration
		}
		sum := sha256.Sum256(data)
		bundle.Files = append(bundle.Files, SkillFile{Path: name, SHA256: hex.EncodeToString(sum[:]), Data: data})
	}
	found := false
	for _, file := range bundle.Files {
		found = found || file.Path == "pgcat-replacement/SKILL.md"
	}
	if !found {
		return nil, ErrConfiguration
	}
	return json.Marshal(bundle)
}
