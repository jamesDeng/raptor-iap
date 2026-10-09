package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyExplicitLoopbackHTTP(t *testing.T) {
	for _, tc := range []struct {
		url       string
		allow, ok bool
	}{{"http://127.0.0.1:8080", true, true}, {"http://127.0.0.1:8080", false, false}, {"http://example.com", true, false}, {"https://raptor.example", false, true}, {"https://user:pass@raptor.example", false, false}, {"https://raptor.example/path", false, false}, {"https://raptor.example?x=1", false, false}} {
		c := Config{Origin: tc.url, AllowLoopbackHTTP: tc.allow, ApplicationCode: "app", EnvironmentCode: "dev", Model: "gpt-5.6-luna", CredentialsFile: "/tmp/private", StateDir: "/tmp/state"}
		if (c.Validate() == nil) != tc.ok {
			t.Fatalf("url %s", tc.url)
		}
	}
}
func TestPrivateConfigRejectsSymlinkAndBroadModes(t *testing.T) {
	d := t.TempDir()
	p := d + "/config.json"
	b := []byte(`{"origin":"https://raptor.example","applicationCode":"app","environmentCode":"dev","model":"gpt-5.6-luna","credentialsFile":"/tmp/private","stateDir":"/tmp/state"}`)
	os.WriteFile(p, b, 0600)
	if _, e := Load(p); e != nil {
		t.Fatal(e)
	}
	os.Chmod(p, 0644)
	if _, e := Load(p); e == nil {
		t.Fatal("broad mode accepted")
	}
	os.Chmod(p, 0600)
	q := filepath.Join(d, "link")
	os.Symlink(p, q)
	if _, e := Load(q); e == nil {
		t.Fatal("symlink accepted")
	}
}
