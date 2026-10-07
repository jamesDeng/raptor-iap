package runtime

import (
	fc "github.com/alibabacloud-go/fcsandbox-20260509/client"
	"github.com/alibabacloud-go/tea/dara"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateConfigRejectsPublicSymlinkAndTrailingData(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	for _, tc := range []struct {
		body string
		mode os.FileMode
	}{{`{"region":"ap-southeast-1"}`, 0644}, {`{"region":"ap-southeast-1"} {}`, 0600}, {`{"unexpected":true}`, 0600}} {
		os.WriteFile(file, []byte(tc.body), tc.mode)
		os.Chmod(file, tc.mode)
		var c LiveConfig
		if ReadPrivateJSON(file, &c) == nil {
			t.Fatal("unsafe config accepted")
		}
	}
	os.WriteFile(file, []byte(`{"region":"ap-southeast-1"}`), 0600)
	os.Chmod(file, 0600)
	link := filepath.Join(dir, "link")
	os.Symlink(file, link)
	var c LiveConfig
	if ReadPrivateJSON(link, &c) == nil {
		t.Fatal("symlink accepted")
	}
	if e := ReadPrivateJSON(file, &c); e != nil || c.Region != "ap-southeast-1" {
		t.Fatal(e)
	}
}
func TestRuntimeModeExplicitAndConflictsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		mode, legacy, want string
		bad                bool
	}{{"", "", "disabled", false}, {"", "true", "simulated", false}, {"live", "true", "", true}, {"disabled", "true", "", true}, {"live", "", "live", false}, {"unknown", "", "", true}} {
		got, e := RuntimeMode(tc.mode, tc.legacy)
		if (e != nil) != tc.bad || got != tc.want {
			t.Fatal(tc, got, e)
		}
	}
}
func TestVolumeAcceptsExistingHTTPSOptionalMountShape(t *testing.T) {
	c := LiveConfig{VolumeID: "volume", VolumeName: "auth", TeamID: "team", AccountID: "account", Bucket: "bucket", BucketPrefix: "auth", ExecutionRoleARN: "role"}
	v := &fc.E2BVolume{VolumeID: dara.String(c.VolumeID), VolumeName: dara.String(c.VolumeName), TeamID: dara.String(c.TeamID), UserID: dara.String(c.AccountID), Status: dara.String("AVAILABLE"), StorageClass: dara.String("OSS"), OssVolumeConfig: &fc.OSSVolumeConfig{BucketName: dara.String(c.Bucket), BucketPath: dara.String("/auth"), Endpoint: dara.String("https://oss-ap-southeast-1.aliyuncs.com"), ReadOnly: dara.Bool(false)}}
	if e := validateVolume(v, c); e != nil {
		t.Fatal(e)
	}
	v.MountConfig = &fc.E2BVolumeMountConfig{Role: dara.String("wrong")}
	if validateVolume(v, c) == nil {
		t.Fatal("wrong role accepted")
	}
}
