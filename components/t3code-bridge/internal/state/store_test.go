package state

import (
	"encoding/json"
	"os"
	"testing"
)

func TestStateExclusiveLock(t *testing.T) {
	p := t.TempDir() + "/state"
	s, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if v, e := Open(p); e == nil {
		v.Close()
		t.Fatal("second owner admitted")
	}
}
func TestIdentityChangeRefused(t *testing.T) {
	s, _ := Open(t.TempDir() + "/state")
	defer s.Close()
	b := Binding{Origin: "https://example.com", UserID: "u", App: "app", Env: "dev", Model: "gpt-5.6-luna"}
	v, _ := s.Create(b)
	v.Payload = json.RawMessage(`{"x":1}`)
	v.Key = "saved"
	s.Save(v)
	b.Env = "prod"
	if _, e := s.Load(v.ID, b); e == nil {
		t.Fatal("changed binding accepted")
	}
}
func TestPrivateAtomicMappingAndTraversal(t *testing.T) {
	p := t.TempDir() + "/state"
	s, _ := Open(p)
	defer s.Close()
	v, _ := s.Create(Binding{})
	if _, e := s.Load("../escape", Binding{}); e == nil {
		t.Fatal("traversal")
	}
	st, _ := os.Stat(p + "/" + v.ID + ".json")
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	if _, e := s.Load(v.ID, Binding{}); e != nil {
		t.Fatal(e)
	}
}
