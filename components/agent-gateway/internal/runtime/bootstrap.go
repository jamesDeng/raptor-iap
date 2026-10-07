package runtime

import (
	"context"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	fc "github.com/alibabacloud-go/fcsandbox-20260509/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"net/http"
	"strings"
	"time"
)

func RuntimeMode(mode, legacy string) (string, error) {
	if mode == "" {
		if legacy == "true" {
			return "simulated", nil
		}
		return "disabled", nil
	}
	if mode != "disabled" && mode != "simulated" && mode != "live" {
		return "", ErrConfiguration
	}
	if legacy == "true" && mode != "simulated" {
		return "", ErrConfiguration
	}
	return mode, nil
}

type fixedOSS struct{ host string }

func (f fixedOSS) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != f.host {
		return nil, ErrConfiguration
	}
	return http.DefaultTransport.RoundTrip(r)
}
func NewCloudClients(c LiveConfig, credential ControllerCredential) (Management, CheckpointVerifier, error) {
	if c.Validate() != nil || credential.AccessKeyID == "" || credential.AccessKeySecret == "" || credential.SecurityToken == "" {
		return Management{}, CheckpointVerifier{}, ErrConfiguration
	}
	client, e := fc.NewClient(&openapi.Config{AccessKeyId: dara.String(credential.AccessKeyID), AccessKeySecret: dara.String(credential.AccessKeySecret), SecurityToken: dara.String(credential.SecurityToken), RegionId: dara.String(c.Region), Endpoint: dara.String("fcsandbox.ap-southeast-1.aliyuncs.com"), Protocol: dara.String("HTTPS")})
	if e != nil {
		return Management{}, CheckpointVerifier{}, ErrConfiguration
	}
	host := c.Bucket + ".oss-ap-southeast-1.aliyuncs.com"
	ossClient, e := oss.New("https://oss-ap-southeast-1.aliyuncs.com", credential.AccessKeyID, credential.AccessKeySecret, oss.SecurityToken(credential.SecurityToken), oss.HTTPClient(&http.Client{Transport: fixedOSS{host: host}, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if e != nil {
		return Management{}, CheckpointVerifier{}, ErrConfiguration
	}
	bucket, e := ossClient.Bucket(c.Bucket)
	if e != nil {
		return Management{}, CheckpointVerifier{}, ErrConfiguration
	}
	return Management{Client: client, TeamID: c.TeamID, AccountID: c.AccountID}, CheckpointVerifier{Objects: OSSObjects{Client: ossClient, Bucket: bucket, BucketName: c.Bucket}, Prefix: c.BucketPrefix}, nil
}
func (m Management) Preflight(ctx context.Context, c LiveConfig) error {
	if m.Client == nil || c.Validate() != nil {
		return ErrConfiguration
	}
	client := *m.Client
	client.HttpClient = guardedSDKHTTP{ctx: ctx, origin: "fcsandbox.ap-southeast-1.aliyuncs.com"}
	r, e := client.GetVolumeWithOptions(dara.String(c.VolumeID), &fc.GetVolumeRequest{TeamID: dara.String(c.TeamID)}, nil, sdkOptions())
	if e != nil || r == nil || r.Body == nil {
		return ErrConfiguration
	}
	return validateVolume(r.Body.Volume, c)
}
func validateVolume(v *fc.E2BVolume, c LiveConfig) error {
	if v == nil || dara.StringValue(v.VolumeID) != c.VolumeID || dara.StringValue(v.VolumeName) != c.VolumeName || dara.StringValue(v.TeamID) != c.TeamID || dara.StringValue(v.UserID) != c.AccountID || dara.StringValue(v.Status) != "AVAILABLE" || dara.StringValue(v.StorageClass) != "OSS" || v.OssVolumeConfig == nil || (v.MountConfig != nil && dara.StringValue(v.MountConfig.Role) != c.ExecutionRoleARN) {
		return ErrConfiguration
	}
	o := v.OssVolumeConfig
	if dara.StringValue(o.BucketName) != c.Bucket || strings.Trim(dara.StringValue(o.BucketPath), "/") != c.BucketPrefix || o.ReadOnly == nil || dara.BoolValue(o.ReadOnly) || dara.StringValue(o.Endpoint) != "https://oss-ap-southeast-1.aliyuncs.com" {
		return ErrConfiguration
	}
	return nil
}
