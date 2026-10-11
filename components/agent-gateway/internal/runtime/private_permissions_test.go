package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateRuntimeFilesAreRestrictedBeforeInference(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "observation.json")
	if os.WriteFile(file, []byte(`{}`), 0644) != nil {
		t.Fatal("fixture")
	}
	script := strings.ReplaceAll(privatePermissionsCommand, "/tmp/raptor-private/", root+"/")
	if err := exec.Command("/bin/sh", "-c", script).Run(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal("private material remained readable")
	}
	os.Remove(file)
	os.Symlink(filepath.Join(root, "attempt-job.json"), file)
	if exec.Command("/bin/sh", "-c", script).Run() == nil {
		t.Fatal("symlink accepted")
	}
}
