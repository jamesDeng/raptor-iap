package skills

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ReleaseSource interface {
	List(context.Context) ([]domain.SkillsVersion, error)
}
type FixtureSource struct{ Versions []domain.SkillsVersion }

func (s FixtureSource) List(context.Context) ([]domain.SkillsVersion, error) {
	return append([]domain.SkillsVersion{}, s.Versions...), nil
}

type GitSource struct{ Repository string }

func (s GitSource) List(ctx context.Context) ([]domain.SkillsVersion, error) {
	if s.Repository == "" {
		return nil, domain.ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, "git", "-C", s.Repository, "tag", "--list", "skills-v*")
	b, e := cmd.Output()
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	out := []domain.SkillsVersion{}
	for _, tag := range strings.Fields(string(b)) {
		if !releasePattern.MatchString(tag) {
			continue
		}
		sha, e := exec.CommandContext(ctx, "git", "-C", s.Repository, "rev-parse", tag+"^{commit}").Output()
		if e != nil {
			return nil, domain.ErrUnavailable
		}
		commit := strings.TrimSpace(string(sha))
		if exec.CommandContext(ctx, "git", "-C", s.Repository, "cat-file", "-e", commit+":agent-skills").Run() != nil {
			continue
		}
		out = append(out, domain.SkillsVersion{Tag: tag, CommitSHA: commit})
	}
	return out, nil
}

var releasePattern = regexp.MustCompile(`^skills-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func releaseLess(a, b string) bool {
	aa, bb := releasePattern.FindStringSubmatch(a), releasePattern.FindStringSubmatch(b)
	for i := 1; i < 4; i++ {
		x, _ := strconv.ParseUint(aa[i], 10, 64)
		y, _ := strconv.ParseUint(bb[i], 10, 64)
		if x != y {
			return x < y
		}
	}
	return false
}
func (s *Service) Resolve(ctx context.Context, tag string) (domain.SkillsVersion, error) {
	if s.Source == nil {
		return domain.SkillsVersion{}, domain.ErrUnavailable
	}
	all, e := s.Source.List(ctx)
	if e != nil {
		return domain.SkillsVersion{}, domain.ErrUnavailable
	}
	valid := []domain.SkillsVersion{}
	for _, v := range all {
		if releasePattern.MatchString(v.Tag) && commitPattern.MatchString(v.CommitSHA) {
			valid = append(valid, v)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return releaseLess(valid[i].Tag, valid[j].Tag) })
	if tag == "" && len(valid) > 0 {
		return valid[len(valid)-1], nil
	}
	for _, v := range valid {
		if v.Tag == tag {
			return v, nil
		}
	}
	return domain.SkillsVersion{}, domain.ErrInvalid
}
func (s *Service) Validate(ctx context.Context, in domain.SkillsVersion) (domain.SkillsVersion, error) {
	v, e := s.Resolve(ctx, in.Tag)
	if e != nil {
		return v, e
	}
	if in.CommitSHA != "" && in.CommitSHA != v.CommitSHA {
		return v, domain.ErrConflict
	}
	return v, nil
}
