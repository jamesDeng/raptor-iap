package store

import (
	"context"
	"errors"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	osscred "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	rolecred "github.com/aliyun/credentials-go/credentials"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"time"
)

type ecsReader struct{ client *oss.Client }

func NewECSReader() (ObjectReader, error) {
	role, e := rolecred.NewCredential(new(rolecred.Config).SetType("ecs_ram_role").SetDisableIMDSv1(true).SetTimeout(5000).SetConnectTimeout(3000))
	if e != nil {
		return nil, errors.New("role_configuration_failed")
	}
	provider := osscred.CredentialsProviderFunc(func(ctx context.Context) (osscred.Credentials, error) {
		if ctx.Err() != nil {
			return osscred.Credentials{}, errors.New("role_cancelled")
		}
		c, e := role.GetCredential()
		if e != nil || c == nil {
			return osscred.Credentials{}, errors.New("role_unavailable")
		}
		return osscred.Credentials{AccessKeyID: oss.ToString(c.AccessKeyId), AccessKeySecret: oss.ToString(c.AccessKeySecret), SecurityToken: oss.ToString(c.SecurityToken)}, nil
	})
	cfg := productionConfig(provider)
	return &ecsReader{client: oss.NewClient(cfg)}, nil
}
func (r *ecsReader) Get(ctx context.Context, ref config.Reference) (Object, error) {
	result, e := r.client.GetObject(ctx, &oss.GetObjectRequest{Bucket: oss.Ptr(ref.Bucket), Key: oss.Ptr(ref.Key), VersionId: oss.Ptr(ref.Version)})
	if e != nil {
		return Object{}, errors.New("oss_request_failed")
	}
	return Object{Body: result.Body, Version: oss.ToString(result.VersionId), Encryption: oss.ToString(result.ServerSideEncryption)}, nil
}

func productionConfig(provider osscred.CredentialsProvider) *oss.Config {
	return oss.LoadDefaultConfig().WithLogLevel(oss.LogOff).WithRegion("ap-southeast-1").WithUseInternalEndpoint(true).WithCredentialsProvider(provider).WithEnabledRedirect(false).WithRetryMaxAttempts(1).WithConnectTimeout(5 * time.Second).WithReadWriteTimeout(10 * time.Second)
}
