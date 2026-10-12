package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrivatePATClientLoadsTokenWithoutLeakingErrors(t *testing.T) {
	const token = "github_pat_private_fixture"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/private/github-pat" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		u, p, _ := r.BasicAuth()
		if u != "gateway" || p != "secret" {
			t.Fatal("dedicated identity missing")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{"token":"`+token+`"}}`)
	}))
	defer server.Close()
	client := PrivatePATClient{BaseURL: server.URL, Username: "gateway", Password: "secret", AllowClusterHTTP: true}
	got, err := client.Load(context.Background())
	if err != nil || string(got) != token {
		t.Fatalf("load: %v", err)
	}
	client.BaseURL = server.URL + "/foreign"
	_, err = client.Load(context.Background())
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("error leak: %v", err)
	}
}

func TestPrivatePATFilesContainTokenOnlyInPrivatePayloads(t *testing.T) {
	const token = "github_pat_private_fixture"
	files, err := privatePATFiles([]byte(token))
	if err != nil {
		t.Fatal(err)
	}
	if string(files["/tmp/raptor-private/github-pat"]) != token {
		t.Fatal("token missing")
	}
	for path, raw := range files {
		if !strings.HasPrefix(path, "/tmp/raptor-private/") {
			t.Fatalf("foreign path: %s", path)
		}
		if path != "/tmp/raptor-private/github-pat" && path != "/tmp/raptor-private/gh/hosts.yml" && strings.Contains(string(raw), token) {
			t.Fatalf("token in %s", path)
		}
	}
	if !strings.Contains(string(files["/tmp/raptor-private/gitconfig"]), "credential") {
		t.Fatal("Git helper not configured")
	}
}
