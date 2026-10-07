package transportpolicy

import "testing"

func TestExplicitClusterOrigins(t *testing.T) {
	for _, origin := range []string{"http://agent-gateway:8874", "http://raptor-backend:8871"} {
		if Allowed(origin, origin, false) {
			t.Fatal("cluster HTTP enabled without explicit opt-in")
		}
		if !Allowed(origin, origin, true) {
			t.Fatal("rendered service origin rejected")
		}
		for _, bad := range []string{"http://evil.example:8874", "http://agent-gateway:80", "http://agent-gateway:8874/path", "http://agent-gateway:8874?secret=x", "http://user@agent-gateway:8874"} {
			if Allowed(bad, origin, true) {
				t.Fatal("untrusted origin accepted", bad)
			}
		}
	}
	if !Allowed("https://api.example", "", false) || !Allowed("http://127.0.0.1:8874", "", false) {
		t.Fatal("existing secure/local transport rejected")
	}
}
