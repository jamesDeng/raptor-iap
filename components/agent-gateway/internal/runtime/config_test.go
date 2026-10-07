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

func TestPrivateLiveConfigSeparatesInfraCredentials(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	raw := `{"accountId":"1360282071200743","region":"ap-southeast-1","teamId":"team","templateId":"template","bucket":"bucket","bucketPrefix":"auth","volumeName":"volume","volumeId":"volume","executionRoleArn":"acs:ram::1360282071200743:role/runtime","raptorMcpUrl":"https://raptor.fixture/mcp","infraMcpUrl":"https://infra.fixture/mcp","harnessDir":"/opt/raptor-harness","infraUsername":"reader","infraPassword":"fixture-only"}`
	os.WriteFile(file, []byte(raw), 0600)
	var c LiveConfig
	if e := ReadPrivateJSON(file, &c); e != nil {
		t.Fatal("separate Infra credentials not accepted", e)
	}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
}

func TestLiveConfigRejectsMissingInfraCredential(t *testing.T){file:=filepath.Join(t.TempDir(),"config.json");raw:=`{"accountId":"1360282071200743","region":"ap-southeast-1","teamId":"team","templateId":"template","bucket":"bucket","bucketPrefix":"auth","volumeName":"volume","volumeId":"volume","executionRoleArn":"acs:ram::1360282071200743:role/runtime","raptorMcpUrl":"https://raptor.fixture/mcp","infraMcpUrl":"https://infra.fixture/mcp","harnessDir":"/opt/raptor-harness"}`;os.WriteFile(file,[]byte(raw),0600);var c LiveConfig;ReadPrivateJSON(file,&c);if c.Validate()==nil{t.Fatal("missing Infra credential accepted")}}
