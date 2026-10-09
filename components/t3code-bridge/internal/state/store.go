package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/config"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
)

var ErrState = errors.New("InvalidSessionState")

type Binding struct{ Origin, UserID, App, Env, Model string }
type Message struct {
	Role   string `json:"role"`
	Text   string `json:"text"`
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
}
type Session struct {
	Version      int             `json:"version"`
	ID           string          `json:"id"`
	Binding      Binding         `json:"binding"`
	Transcript   []Message       `json:"transcript"`
	Key          string          `json:"key"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	RequestID    string          `json:"requestId"`
	AttemptID    string          `json:"attemptId"`
	Cursor       int64           `json:"cursor"`
	CancelIntent bool            `json:"cancelIntent"`
	CancelSent   bool            `json:"cancelSent"`
	Terminal     bool            `json:"terminal"`
	Outcome      string          `json:"outcome"`
}
type Store struct {
	dir  string
	lock *os.File
	mu   sync.Mutex
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Open(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, ErrState
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, ErrState
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 {
		return nil, ErrState
	}
	fd, e := syscall.Open(filepath.Join(dir, "owner.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, ErrState
	}
	f := os.NewFile(uintptr(fd), "owner.lock")
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("BridgeAlreadyRunning")
	}
	return &Store{dir: dir, lock: f}, nil
}
func (s *Store) Close() error { return s.lock.Close() }
func (s *Store) Create(b Binding) (Session, error) {
	v := Session{Version: 1, ID: ID(), Binding: b, Transcript: []Message{}}
	return v, s.Save(v)
}
func (s *Store) load(id string, b Binding) (Session, error) {
	var v Session
	if !idPattern.MatchString(id) {
		return v, ErrState
	}
	raw, e := config.PrivateRead(filepath.Join(s.dir, id+".json"))
	if e != nil || json.Unmarshal(raw, &v) != nil || v.Version != 1 || v.ID != id || v.Binding != b || v.Cursor < 0 {
		return v, ErrState
	}
	return v, nil
}
func (s *Store) Load(id string, b Binding) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id, b)
}
func (s *Store) save(v Session) error {
	if !idPattern.MatchString(v.ID) || v.Version != 1 {
		return ErrState
	}
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 1<<20 {
		return ErrState
	}
	f, e := os.CreateTemp(s.dir, ".pending-")
	if e != nil {
		return ErrState
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return ErrState
	}
	if e = os.Rename(tmp, filepath.Join(s.dir, v.ID+".json")); e != nil {
		return ErrState
	}
	d, e := os.Open(s.dir)
	if e != nil {
		return ErrState
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrState
	}
	return nil
}
func (s *Store) Save(v Session) error { s.mu.Lock(); defer s.mu.Unlock(); return s.save(v) }
func (s *Store) Update(id string, b Binding, fn func(*Session) error) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.load(id, b)
	if e != nil {
		return v, e
	}
	if e = fn(&v); e != nil {
		return v, e
	}
	return v, s.save(v)
}
