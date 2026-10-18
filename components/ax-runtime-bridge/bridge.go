// Copyright 2026 Raptor IAP contributors.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ErrAbsent = errors.New("ResourceAbsent")
var ErrUnknown = errors.New("OutcomeUnconfirmed")
var ErrConflict = errors.New("OwnershipConflict")
var ErrInvalid = errors.New("InvalidRuntimeRequest")

type Binding struct {
	DefinitionSHA256  string `json:"definitionSha256"`
	ClusterID         string `json:"clusterId"`
	RequestID         string `json:"requestId"`
	AttemptID         string `json:"attemptId"`
	Operation         string `json:"operation"`
	ObjectKind        string `json:"objectKind"`
	ObjectCode        string `json:"objectCode"`
	EnvCode           string `json:"envCode"`
	SkillsCommit      string `json:"skillsCommit"`
	Model             string `json:"model"`
	ExecutionMode     string `json:"executionMode,omitempty"`
	ProviderID        string `json:"providerId,omitempty"`
	ConnectionVersion int64  `json:"connectionVersion,omitempty"`
}
type Request struct {
	Action       string       `json:"action"`
	Binding      Binding      `json:"binding"`
	Repositories []Repository `json:"repositories,omitempty"`
	Deadline     time.Time    `json:"deadline"`
	Phase        string       `json:"phase,omitempty"`
	Path         string       `json:"path,omitempty"`
	Data         []byte       `json:"data,omitempty"`
	Limit        int64        `json:"limit,omitempty"`
}
type Repository struct {
	EnvironmentCode string `json:"environmentCode"`
	Purpose         string `json:"purpose"`
	URL             string `json:"url"`
	Directory       string `json:"directory"`
	BaseBranch      string `json:"baseBranch"`
	BaseCommit      string `json:"baseCommit"`
}
type Response struct {
	Name      string `json:"name,omitempty"`
	ProcessID string `json:"processId,omitempty"`
	Terminal  bool   `json:"terminal"`
	ExitCode  int    `json:"exitCode"`
	Data      []byte `json:"data,omitempty"`
	Absent    bool   `json:"absent"`
	Error     string `json:"error,omitempty"`
}
type TaskView struct {
	Name, Seal, Image, Phase, Workspace string
	Ready                               bool
}
type Backend interface {
	Get(context.Context, string) (*TaskView, error)
	Create(context.Context, string, string) (*TaskView, error)
	Resume(context.Context, string) (*TaskView, error)
	Delete(context.Context, string) error
	ActorAbsent(context.Context, string) (bool, error)
	Run(context.Context, string, string) (string, error)
	Process(context.Context, string, string) (bool, int, error)
	Read(context.Context, string, string, int64) ([]byte, error)
	Write(context.Context, string, string, []byte) error
}
type RepositoryBackend interface {
	CreateWithRepositories(context.Context, string, string, []Repository) (*TaskView, error)
	DeleteRepositoryWorkspace(context.Context, string, []Repository) error
}
type processRecord struct {
	ID      string `json:"id"`
	Pending bool   `json:"pending"`
}
type state struct {
	Image     string                   `json:"image"`
	Seal      string                   `json:"seal"`
	Confirmed bool                     `json:"confirmed"`
	Processes map[string]processRecord `json:"processes"`
}
type Service struct {
	Backend         Backend
	StateDir, Image string
	mu              sync.Mutex
}

var uuid = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var commit = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var label = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)
var filePath = regexp.MustCompile(`^/[a-zA-Z0-9_./-]{1,512}$`)
var repositoryPath = regexp.MustCompile(`^/[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9][A-Za-z0-9_.-]*(\.git)?$`)
var branchPath = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)

func validRepository(v Repository) bool {
	u, e := url.Parse(v.URL)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || v.URL != "https://github.com"+u.Path || !repositoryPath.MatchString(u.Path) || strings.Contains(u.Path, "..") || (v.Purpose != "terraform" && v.Purpose != "kubernetes") || !branchPath.MatchString(v.BaseBranch) || strings.Contains(v.BaseBranch, "..") || strings.HasPrefix(v.BaseBranch, "-") || strings.HasSuffix(v.BaseBranch, ".") || !commit.MatchString(v.BaseCommit) || v.Directory == "" || path.IsAbs(v.Directory) || path.Clean(v.Directory) != v.Directory || v.Directory == ".." || strings.HasPrefix(v.Directory, "../") || strings.Contains(v.Directory, "\\") {
		return false
	}
	for _, p := range strings.Split(v.BaseBranch, "/") {
		if p == "" || p == "." || strings.HasSuffix(p, ".lock") {
			return false
		}
	}
	return true
}

func (r Request) validate(mutation bool) error {
	b := r.Binding
	modelValid := (b.ProviderID == "" && b.ConnectionVersion == 0 && b.Model == "gpt-5.6-luna") || (b.ProviderID == "codex" && b.ConnectionVersion > 0 && label.MatchString(b.Model))
	pgcat := b.Operation == "db-proxy.replace-nodes" && b.ObjectKind == "db-proxy" && (b.ExecutionMode == "qualification" || b.ExecutionMode == "replacement") && b.EnvCode == "rdev.ali" && b.ProviderID == "codex" && b.ConnectionVersion > 0 && len(r.Repositories) == 0
	application := b.ObjectKind == "application" && b.ExecutionMode == "" && ((b.Operation == "application.question" && b.EnvCode == "rdev.ali") || (b.Operation == "repository.change" && label.MatchString(b.EnvCode)))
	if !uuid.MatchString(b.RequestID) || !uuid.MatchString(b.AttemptID) || (!pgcat && !application) || !label.MatchString(b.ObjectCode) || !commit.MatchString(b.SkillsCommit) || !digest.MatchString(b.DefinitionSHA256) || b.ClusterID == "" || !modelValid {
		return ErrInvalid
	}
	if b.Operation == "application.question" && len(r.Repositories) != 0 {
		return ErrInvalid
	}
	if b.Operation == "repository.change" {
		if len(r.Repositories) == 0 || len(r.Repositories) > 32 {
			return ErrInvalid
		}
		branches := map[string]string{}
		seen := map[string]bool{}
		for _, repo := range r.Repositories {
			if repo.EnvironmentCode != b.EnvCode {
				return ErrInvalid
			}
			if !validRepository(repo) {
				return ErrInvalid
			}
			key := repo.Purpose + "\x00" + repo.URL + "\x00" + repo.Directory
			if seen[key] || (branches[repo.URL] != "" && branches[repo.URL] != repo.BaseBranch) {
				return ErrInvalid
			}
			seen[key] = true
			branches[repo.URL] = repo.BaseBranch
		}
	}
	if mutation && (!r.Deadline.After(time.Now()) || r.Deadline.After(time.Now().Add(16*time.Minute))) {
		return ErrInvalid
	}
	return nil
}
func (r Request) name() string          { return "raptor-" + r.Binding.AttemptID }
func (r Request) workspaceName() string { return "raptor-repositories-" + r.Binding.AttemptID }
func (r Request) ownsTask(t *TaskView, image string) bool {
	if t == nil || t.Name != r.name() || t.Seal != r.seal() || t.Image != image {
		return false
	}
	return r.Binding.Operation != "repository.change" || t.Workspace == r.workspaceName()
}
func (r Request) seal() string {
	var raw []byte
	if r.Binding.Operation == "repository.change" {
		raw, _ = json.Marshal(struct {
			Binding      Binding      `json:"binding"`
			Repositories []Repository `json:"repositories"`
		}{r.Binding, r.Repositories})
	} else {
		raw, _ = json.Marshal(r.Binding)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func safePath(p string) bool {
	return filePath.MatchString(p) && path.Clean(p) == p && (p == "/tmp/raptor-private" || stringsPrefix(p, "/tmp/raptor-private/") || stringsPrefix(p, "/tmp/raptor-oss/"))
}
func stringsPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
func (s *Service) load(r Request) (*state, error) {
	raw, e := os.ReadFile(filepath.Join(s.StateDir, r.name()+".json"))
	if os.IsNotExist(e) {
		return nil, ErrAbsent
	}
	if e != nil {
		return nil, ErrUnknown
	}
	var v state
	if json.Unmarshal(raw, &v) != nil || v.Seal != r.seal() || v.Processes == nil || v.Image == "" {
		return nil, ErrConflict
	}
	return &v, nil
}
func (s *Service) save(r Request, v *state) error {
	if os.MkdirAll(s.StateDir, 0700) != nil {
		return ErrUnknown
	}
	info, e := os.Lstat(s.StateDir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnknown
	}
	f, e := os.CreateTemp(s.StateDir, ".pending-")
	if e != nil {
		return ErrUnknown
	}
	name := f.Name()
	defer os.Remove(name)
	raw, _ := json.Marshal(v)
	if f.Chmod(0600) != nil {
		f.Close()
		return ErrUnknown
	}
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return ErrUnknown
	}
	if f.Sync() != nil {
		f.Close()
		return ErrUnknown
	}
	if f.Close() != nil {
		return ErrUnknown
	}
	if os.Rename(name, filepath.Join(s.StateDir, r.name()+".json")) != nil {
		return ErrUnknown
	}
	dir, e := os.Open(s.StateDir)
	if e != nil {
		return ErrUnknown
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *Service) owned(ctx context.Context, r Request) (*TaskView, *state, error) {
	if e := r.validate(false); e != nil {
		return nil, nil, e
	}
	v, e := s.load(r)
	if e != nil {
		return nil, nil, e
	}
	task, e := s.Backend.Get(ctx, r.name())
	if e != nil {
		return nil, v, e
	}
	if !r.ownsTask(task, v.Image) {
		return nil, v, ErrConflict
	}
	if !v.Confirmed {
		v.Confirmed = true
		if e = s.save(r, v); e != nil {
			return nil, v, e
		}
	}
	return task, v, nil
}
func (s *Service) Ensure(ctx context.Context, r Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Response{Name: r.name()}
	if e := r.validate(true); e != nil {
		return out, e
	}
	var repositoryBackend RepositoryBackend
	if r.Binding.Operation == "repository.change" {
		var ok bool
		repositoryBackend, ok = s.Backend.(RepositoryBackend)
		if !ok {
			return out, ErrInvalid
		}
	}
	v, loadErr := s.load(r)
	task, getErr := s.Backend.Get(ctx, r.name())
	if getErr == nil {
		if loadErr != nil || !r.ownsTask(task, v.Image) {
			return out, ErrConflict
		}
		v.Confirmed = true
		if e := s.save(r, v); e != nil {
			return out, e
		}
	} else {
		if !errors.Is(getErr, ErrAbsent) {
			return out, ErrUnknown
		}
		if loadErr == nil {
			return out, ErrUnknown
		}
		if !errors.Is(loadErr, ErrAbsent) {
			return out, loadErr
		}
		absent, e := s.Backend.ActorAbsent(ctx, r.name())
		if e != nil || !absent {
			return out, ErrConflict
		}
		v = &state{Seal: r.seal(), Image: s.Image, Processes: map[string]processRecord{}}
		if e = s.save(r, v); e != nil {
			return out, e
		}
		if repositoryBackend != nil {
			task, e = repositoryBackend.CreateWithRepositories(ctx, r.name(), r.seal(), r.Repositories)
		} else {
			task, e = s.Backend.Create(ctx, r.name(), r.seal())
		}
		if e != nil {
			task, e = s.Backend.Get(ctx, r.name())
			if e != nil {
				return out, ErrUnknown
			}
		}
		if !r.ownsTask(task, v.Image) {
			return out, ErrConflict
		}
		v.Confirmed = true
		if e = s.save(r, v); e != nil {
			return out, e
		}
	}
	_, e := s.Backend.Resume(ctx, r.name())
	if e != nil {
		return out, ErrUnknown
	}
	readyCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := s.Backend.Get(readyCtx, r.name())
		if err != nil || task == nil || !r.ownsTask(task, v.Image) {
			return out, ErrUnknown
		}
		if task.Ready {
			break
		}
		select {
		case <-readyCtx.Done():
			return out, ErrUnknown
		case <-ticker.C:
		}
	}
	return out, nil
}
func (s *Service) Start(ctx context.Context, r Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Response{Name: r.name()}
	if e := r.validate(true); e != nil {
		return out, e
	}
	if r.Phase != "prepare" && r.Phase != "restore" && r.Phase != "inference" {
		return out, ErrInvalid
	}
	_, v, e := s.owned(ctx, r)
	if e != nil {
		return out, e
	}
	if p, ok := v.Processes[r.Phase]; ok {
		if p.Pending || p.ID == "" {
			return out, ErrUnknown
		}
		out.ProcessID = p.ID
		return out, nil
	}
	if r.Phase != "prepare" {
		previous := "prepare"
		if r.Phase == "inference" {
			previous = "restore"
		}
		p, ok := v.Processes[previous]
		if !ok || p.Pending || p.ID == "" {
			return out, ErrInvalid
		}
		done, code, e := s.Backend.Process(ctx, r.name(), p.ID)
		if e != nil || !done || code != 0 {
			return out, ErrInvalid
		}
	}
	v.Processes[r.Phase] = processRecord{Pending: true}
	if e = s.save(r, v); e != nil {
		return out, e
	}
	id, e := s.Backend.Run(ctx, r.name(), r.Phase)
	if e != nil || id == "" {
		return out, ErrUnknown
	}
	v.Processes[r.Phase] = processRecord{ID: id}
	if e = s.save(r, v); e != nil {
		return out, ErrUnknown
	}
	out.ProcessID = id
	return out, nil
}
func (s *Service) Poll(ctx context.Context, r Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Response{Name: r.name()}
	_, v, e := s.owned(ctx, r)
	if e != nil {
		return out, e
	}
	p, ok := v.Processes[r.Phase]
	if !ok || p.Pending || p.ID == "" {
		return out, ErrUnknown
	}
	out.ProcessID = p.ID
	out.Terminal, out.ExitCode, e = s.Backend.Process(ctx, r.name(), p.ID)
	return out, e
}
func (s *Service) File(ctx context.Context, r Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Response{Name: r.name()}
	if e := r.validate(r.Action == "write"); e != nil {
		return out, e
	}
	if !safePath(r.Path) || r.Limit < 0 || r.Limit > 16*1024*1024 || len(r.Data) > 16*1024*1024 {
		return out, ErrInvalid
	}
	_, v, e := s.owned(ctx, r)
	if e != nil {
		return out, e
	}
	if r.Action == "write" {
		if _, started := v.Processes["inference"]; started && r.Path != "/tmp/raptor-private/attempt-cancel" && r.Path != "/tmp/raptor-private/model-refresh-ack.json" {
			return out, ErrInvalid
		}
	}
	if r.Action == "write" {
		return out, s.Backend.Write(ctx, r.name(), r.Path, r.Data)
	}
	if r.Action != "read" || r.Limit == 0 {
		return out, ErrInvalid
	}
	out.Data, e = s.Backend.Read(ctx, r.name(), r.Path, r.Limit)
	if int64(len(out.Data)) > r.Limit {
		return Response{}, ErrInvalid
	}
	return out, e
}
func (s *Service) Remove(ctx context.Context, r Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Response{Name: r.name()}
	if e := r.validate(false); e != nil {
		return out, e
	}
	v, e := s.load(r)
	if errors.Is(e, ErrAbsent) {
		// Serialize a cleanup tombstone before confirming absence; a delayed ensure
		// cannot arrive afterwards and create an actor for this attempt.
		task, getErr := s.Backend.Get(ctx, r.name())
		if getErr == nil && task != nil {
			return out, ErrConflict
		}
		if !errors.Is(getErr, ErrAbsent) {
			return out, ErrUnknown
		}
		absent, err := s.Backend.ActorAbsent(ctx, r.name())
		if err != nil || !absent {
			return out, ErrUnknown
		}
		v = &state{Seal: r.seal(), Image: s.Image, Confirmed: true, Processes: map[string]processRecord{}}
		if err = s.save(r, v); err != nil {
			return out, err
		}
	} else if e != nil {
		return out, e
	}
	task, e := s.Backend.Get(ctx, r.name())
	if e == nil {
		if !r.ownsTask(task, v.Image) {
			return out, ErrConflict
		}
		v.Confirmed = true
		if e = s.save(r, v); e != nil {
			return out, e
		}
		if e = s.Backend.Delete(ctx, r.name()); e != nil {
			return out, ErrUnknown
		}
	} else if !errors.Is(e, ErrAbsent) {
		return out, ErrUnknown
	}
	if !v.Confirmed {
		return out, ErrUnknown
	}
	if _, e = s.Backend.Get(ctx, r.name()); !errors.Is(e, ErrAbsent) {
		return out, ErrUnknown
	}
	absent, e := s.Backend.ActorAbsent(ctx, r.name())
	if e != nil || !absent {
		return out, ErrUnknown
	}
	if r.Binding.Operation == "repository.change" {
		repositoryBackend, ok := s.Backend.(RepositoryBackend)
		if !ok {
			return out, ErrInvalid
		}
		if e = repositoryBackend.DeleteRepositoryWorkspace(ctx, r.name(), r.Repositories); e != nil {
			return out, ErrUnknown
		}
	}
	// Keep a nonsecret tombstone: a repeated cleanup verifies absence again and
	// a delayed ensure cannot recreate this attempt or replay inference.
	v.Processes = map[string]processRecord{}
	if e = s.save(r, v); e != nil {
		return out, e
	}
	out.Absent = true
	return out, nil
}
