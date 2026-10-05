package scope

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRejectsInvalidScopes(t *testing.T) {
	for _, s := range []string{`[{"Code":"dev","AccountID":"1","Region":"cn-hangzhou"}]`, `[{"Code":"dev","Region":"ap-southeast-1"}]`, `[{"Code":"dev","AccountID":"1","Region":"ap-southeast-1","password":"secret"}]`, `[{"Code":"dev","AccountID":"1","Region":"ap-southeast-1"},{"Code":"dev","AccountID":"1","Region":"ap-southeast-1"}]`} {
		p := filepath.Join(t.TempDir(), "scope.json")
		os.WriteFile(p, []byte(s), 0600)
		if _, e := Load(p); e == nil {
			t.Fatal("accepted invalid scope")
		}
	}
}
