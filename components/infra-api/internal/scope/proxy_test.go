package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyMappingScope(t *testing.T) {
	valid := `[{"code":"rdev.ali","accountId":"1360282071200743","region":"ap-southeast-1","proxies":[{"code":"proxy","groupId":"asg-test","serverGroupId":"sgp-test","listenerId":"lsn-test","port":6432,"targetDbCode":"db"}]}]`
	cases := []struct {
		name, input string
		ok          bool
	}{
		{"valid", valid, true},
		{"missing db", strings.Replace(valid, `"targetDbCode":"db"`, `"targetDbCode":""`, 1), false},
		{"invalid port", strings.Replace(valid, `6432`, `0`, 1), false},
		{"missing listener", strings.Replace(valid, `"lsn-test"`, `""`, 1), false},
		{"foreign region", strings.Replace(valid, `ap-southeast-1`, `cn-hangzhou`, 1), false},
		{"duplicate mapping", strings.Replace(valid, `"targetDbCode":"db"}]`, `"targetDbCode":"db"},{"code":"proxy","groupId":"asg-other","serverGroupId":"sgp-other","listenerId":"lsn-other","port":6432,"targetDbCode":"db"}]`, 1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "scope.json")
			if e := os.WriteFile(p, []byte(tc.input), 0600); e != nil {
				t.Fatal(e)
			}
			_, e := Load(p)
			if (e == nil) != tc.ok {
				t.Fatalf("accepted=%v want=%v", e == nil, tc.ok)
			}
		})
	}
}
