package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/aliyun/credentials-go/credentials"
)

// The OIDC SDK cache is mutable and unsynchronized. All consumers share this lock.
// Never return upstream errors: STS responses can contain sensitive values.
type lockedCredential struct {
	source credentials.Credential
	mu     sync.Mutex
}

func (c *lockedCredential) GetCredential() (result *credentials.CredentialModel, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// credentials-go v1.4.5 dereferences a missing STS Expiration field.
	// Keep malformed external responses inside this SDK boundary.
	defer func() {
		if recover() != nil {
			result = nil
			err = ErrRuntimeUnavailable
		}
	}()
	v, e := c.source.GetCredential()
	if e != nil || v == nil || dara.StringValue(v.AccessKeyId) == "" || dara.StringValue(v.AccessKeySecret) == "" || dara.StringValue(v.SecurityToken) == "" {
		return nil, ErrRuntimeUnavailable
	}
	return &credentials.CredentialModel{AccessKeyId: dara.String(*v.AccessKeyId), AccessKeySecret: dara.String(*v.AccessKeySecret), SecurityToken: dara.String(*v.SecurityToken), Type: dara.String("sts")}, nil
}
func (c *lockedCredential) GetAccessKeyId() (*string, error) {
	v, e := c.GetCredential()
	if e != nil {
		return nil, e
	}
	return v.AccessKeyId, nil
}
func (c *lockedCredential) GetAccessKeySecret() (*string, error) {
	v, e := c.GetCredential()
	if e != nil {
		return nil, e
	}
	return v.AccessKeySecret, nil
}
func (c *lockedCredential) GetSecurityToken() (*string, error) {
	v, e := c.GetCredential()
	if e != nil {
		return nil, e
	}
	return v.SecurityToken, nil
}
func (*lockedCredential) GetBearerToken() *string { return nil }
func (*lockedCredential) GetType() *string        { return dara.String("sts") }
func (*lockedCredential) String() string          { return "[cloud credential provider]" }
func (c *lockedCredential) GoString() string      { return c.String() }

type ossSnapshot struct{ id, secret, token string }

func (s ossSnapshot) GetAccessKeyID() string     { return s.id }
func (s ossSnapshot) GetAccessKeySecret() string { return s.secret }
func (s ossSnapshot) GetSecurityToken() string   { return s.token }
func (s ossSnapshot) String() string             { return "[cloud credential]" }
func (s ossSnapshot) GoString() string           { return s.String() }

type ossCredentialProvider struct{ source *lockedCredential }

func (p ossCredentialProvider) GetCredentialsE() (oss.Credentials, error) {
	v, e := p.source.GetCredential()
	if e != nil {
		return nil, e
	}
	return ossSnapshot{id: *v.AccessKeyId, secret: *v.AccessKeySecret, token: *v.SecurityToken}, nil
}

// OSS v3 uses GetCredentialsE for signing and stops the request on error.
func (p ossCredentialProvider) GetCredentials() oss.Credentials {
	v, e := p.GetCredentialsE()
	if e != nil {
		return ossSnapshot{}
	}
	return v
}

func NewRRSACloudClients(c LiveConfig, roleARN, providerARN, tokenPath string) (Management, CheckpointVerifier, error) {
	if c.Validate() != nil || !strings.HasPrefix(roleARN, "acs:ram::"+c.AccountID+":role/") || !strings.HasPrefix(providerARN, "acs:ram::"+c.AccountID+":oidc-provider/") || !filepath.IsAbs(tokenPath) {
		return Management{}, CheckpointVerifier{}, ErrConfiguration
	}
	// Explicit credentials disable ambient access keys and instance-role fallback.
	source, e := credentials.NewCredential(&credentials.Config{Type: dara.String("oidc_role_arn"), RoleArn: dara.String(roleARN), OIDCProviderArn: dara.String(providerARN), OIDCTokenFilePath: dara.String(tokenPath), RoleSessionName: dara.String("raptor-gateway"), RoleSessionExpiration: dara.Int(3600), STSEndpoint: dara.String("sts.ap-southeast-1.aliyuncs.com")})
	if e != nil {
		return Management{}, CheckpointVerifier{}, ErrConfiguration
	}
	return newCloudClients(c, &lockedCredential{source: source})
}

// CloudClientsFromEnvironment keeps static STS compatible while requiring an
// explicit mode for RRSA; neither mode falls back to another identity.
func CloudClientsFromEnvironment(c LiveConfig) (Management, CheckpointVerifier, error) {
	switch os.Getenv("GATEWAY_CONTROLLER_CREDENTIAL_MODE") {
	case "rrsa":
		if os.Getenv("GATEWAY_CONTROLLER_CREDENTIAL_FILE") != "" {
			return Management{}, CheckpointVerifier{}, ErrConfiguration
		}
		return NewRRSACloudClients(c, os.Getenv("ALIBABA_CLOUD_ROLE_ARN"), os.Getenv("ALIBABA_CLOUD_OIDC_PROVIDER_ARN"), os.Getenv("ALIBABA_CLOUD_OIDC_TOKEN_FILE"))
	case "", "static-sts":
		var v ControllerCredential
		if ReadPrivateJSON(os.Getenv("GATEWAY_CONTROLLER_CREDENTIAL_FILE"), &v) != nil {
			return Management{}, CheckpointVerifier{}, ErrConfiguration
		}
		return NewCloudClients(c, v)
	default:
		return Management{}, CheckpointVerifier{}, ErrConfiguration
	}
}
