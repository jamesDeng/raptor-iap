package runtime

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"strings"
	"testing"
)

func TestObserverSQLSourceRequiresEncryptedBoundedBoundIdentity(t *testing.T) {
	raw := `{"version":1,"envCode":"rdev.ali","targetDbCode":"db-code","dbInstanceId":"db-instance","user":"probe","password":"private-password","database":"test","sslmode":"disable"}`
	scope := execution.ReplacementScope{EnvCode: "rdev.ali", TargetDBCode: "db-code"}
	in := execution.LiveStart{Binding: execution.AttemptBinding{EnvCode: "rdev.ali"}, Access: execution.AgentAccess{ReplacementScope: &scope}}
	for _, kind := range []string{"good", "plaintext", "foreign", "oversized", "extra"} {
		f := fixtureObjects{archive: raw, size: int64(len(raw)), encryption: "AES256"}
		if kind == "plaintext" {
			f.encryption = ""
		}
		if kind == "foreign" {
			f.archive = strings.Replace(raw, "db-code", "foreign", 1)
			f.size = int64(len(f.archive))
		}
		if kind == "oversized" {
			f.size = 65537
		}
		if kind == "extra" {
			f.archive = strings.TrimSuffix(raw, "}") + `,"admin":true}`
			f.size = int64(len(f.archive))
		}
		source := ObserverSQLSource{Objects: f, Key: "observer/sql.json", EnvCode: "rdev.ali", DBCode: "db-code", DBInstanceID: "db-instance"}
		credential, err := source.Load(context.Background(), in)
		if kind == "good" {
			if err != nil || credential.Password != "private-password" {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("unsafe source accepted", kind)
		}
		if strings.Contains(credential.String(), "private-password") {
			t.Fatal("credential exposed")
		}
	}
}
