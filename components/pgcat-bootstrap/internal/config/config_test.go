package config

import (
	"encoding/json"
	"strings"
	"testing"
)

const revision = "0123456789012345678901234567890123456789"

func metadata() string {
	return `{"env":"rdev.ali","proxy_code":"proxy","target_db_code":"db","db_host":"db.internal","database":"test","secret_reference":"oss://private-bucket/config/proxy.json?versionId=v1","revision":"` + revision + `"}`
}
func TestMetadata(t *testing.T) {
	m, err := ParseMetadata([]byte(metadata()), revision)
	if err != nil || m.Reference.Version != "v1" {
		t.Fatalf("valid metadata: %v", err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"version missing", "?versionId=v1", ""}, {"duplicate version", "?versionId=v1", "?versionId=v1&versionId=v2"},
		{"unknown query", "?versionId=v1", "?versionId=v1&token=secret"}, {"scheme", "oss://", "https://"},
		{"revision", revision, strings.Repeat("a", 40)}, {"empty env", "rdev.ali", ""}, {"unknown field", "{", "{\"unknown\":1,"},
		{"duplicate field", "{", "{\"env\":\"other\","}, {"trailing", "}", "} {}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := ParseMetadata([]byte(strings.Replace(metadata(), tc.from, tc.to, 1)), revision); e == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
}
func TestBundle(t *testing.T) {
	m, _ := ParseMetadata([]byte(metadata()), revision)
	valid := map[string]any{"schema_version": 1, "env": m.Env, "proxy_code": m.ProxyCode, "target_db_code": m.TargetDBCode, "db_host": m.DBHost, "database": m.Database, "tls_hostname": "proxy.internal", "app_user": "app", "app_password": "SYNTHETIC_SECRET", "admin_user": "admin", "admin_password": "SYNTHETIC_ADMIN", "server_certificate": "cert", "server_private_key": "key", "client_ca": "ca", "backend_ca": "ca"}
	data, _ := json.Marshal(valid)
	if _, e := ParseBundle(data, m); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"env", "proxy_code", "target_db_code", "db_host", "database", "app_password"} {
		t.Run(key, func(t *testing.T) {
			copy := map[string]any{}
			for k, v := range valid {
				copy[k] = v
			}
			copy[key] = ""
			b, _ := json.Marshal(copy)
			_, e := ParseBundle(b, m)
			if e == nil {
				t.Fatal("accepted invalid bundle")
			}
			if strings.Contains(e.Error(), "SYNTHETIC") {
				t.Fatal("leaked secret")
			}
		})
	}
	for _, b := range [][]byte{[]byte(strings.Repeat("x", 256*1024+1)), []byte(`{"schema_version":1,"schema_version":1}`), []byte(`{"unknown":1}`)} {
		if _, e := ParseBundle(b, m); e == nil {
			t.Fatal("accepted malformed bundle")
		}
	}
}

func TestPrivatePOCBundleNeedsNoTLSMaterial(t *testing.T) {
	m, _ := ParseMetadata([]byte(metadata()), revision)
	data := []byte(`{"schema_version":1,"env":"rdev.ali","proxy_code":"proxy","target_db_code":"db","db_host":"db.internal","database":"test","app_user":"app","app_password":"synthetic","admin_user":"admin","admin_password":"synthetic"}`)
	if _, e := ParseBundle(data, m); e != nil {
		t.Fatalf("private POC bundle rejected: %v", e)
	}
}
