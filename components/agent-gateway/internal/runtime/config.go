package runtime

import (
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type LiveConfig struct {
	AccountID           string                       `json:"accountId"`
	Region              string                       `json:"region"`
	TeamID              string                       `json:"teamId"`
	TemplateID          string                       `json:"templateId"`
	Bucket              string                       `json:"bucket"`
	BucketPrefix        string                       `json:"bucketPrefix"`
	VolumeName          string                       `json:"volumeName"`
	VolumeID            string                       `json:"volumeId"`
	ExecutionRoleARN    string                       `json:"executionRoleArn"`
	BootstrapCheckpoint execution.VerifiedCheckpoint `json:"bootstrapCheckpoint"`
	RaptorMcpURL        string                       `json:"raptorMcpUrl"`
	InfraMcpURL         string                       `json:"infraMcpUrl"`
	HarnessDir          string                       `json:"harnessDir"`
}
type ControllerCredential struct {
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	SecurityToken   string `json:"securityToken"`
}

func (c ControllerCredential) String() string   { return "[controller credential]" }
func (c ControllerCredential) GoString() string { return c.String() }
func ReadPrivateJSON(file string, value any) error {
	info, e := os.Lstat(file)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return ErrConfiguration
	}
	f, e := os.Open(file)
	if e != nil {
		return ErrConfiguration
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return ErrConfiguration
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrConfiguration
	}
	return nil
}
func (c LiveConfig) Validate() error {
	if c.Region != "ap-southeast-1" || !regexp.MustCompile(`^[0-9]{12,20}$`).MatchString(c.AccountID) || !transportID.MatchString(c.TeamID) || !transportID.MatchString(c.TemplateID) || !transportID.MatchString(c.VolumeID) || c.VolumeName == "" || c.Bucket == "" || c.BucketPrefix == "" || strings.HasPrefix(c.BucketPrefix, "/") || strings.Contains(c.BucketPrefix, "..") || !strings.HasPrefix(c.ExecutionRoleARN, "acs:ram::"+c.AccountID+":role/") || c.HarnessDir == "" {
		return ErrConfiguration
	}
	for _, endpoint := range []string{c.RaptorMcpURL, c.InfraMcpURL} {
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/mcp") {
			return ErrConfiguration
		}
	}
	return nil
}
