package githubcredentials

import (
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/aliyun/credentials-go/credentials"
)

type oidcCredentials struct {
	source credentials.Credential
	mu     sync.Mutex
}
type stsSnapshot struct{ id, secret, token string }

func (s stsSnapshot) GetAccessKeyID() string     { return s.id }
func (s stsSnapshot) GetAccessKeySecret() string { return s.secret }
func (s stsSnapshot) GetSecurityToken() string   { return s.token }
func (s stsSnapshot) String() string             { return "[cloud credential]" }
func (s stsSnapshot) GoString() string           { return s.String() }

func (p *oidcCredentials) GetCredentialsE() (out oss.Credentials, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() {
		if recover() != nil {
			out = nil
			err = ErrUnavailable
		}
	}()
	v, e := p.source.GetCredential()
	if e != nil || v == nil || dara.StringValue(v.AccessKeyId) == "" || dara.StringValue(v.AccessKeySecret) == "" || dara.StringValue(v.SecurityToken) == "" {
		return nil, ErrUnavailable
	}
	return stsSnapshot{*v.AccessKeyId, *v.AccessKeySecret, *v.SecurityToken}, nil
}
func (p *oidcCredentials) GetCredentials() oss.Credentials {
	v, e := p.GetCredentialsE()
	if e != nil {
		return stsSnapshot{}
	}
	return v
}

type fixedOSSTransport struct{ host string }

func (t fixedOSSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != t.host {
		return nil, ErrInvalid
	}
	return http.DefaultTransport.RoundTrip(r)
}

var ossBucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// NewRRSAOSSSecrets requires an explicit role and OIDC identity; it never uses ambient keys.
func NewRRSAOSSSecrets(accountID, bucketName, roleARN, providerARN, tokenPath string) (OSSSecrets, error) {
	if !ossBucketName.MatchString(bucketName) || accountID == "" || !strings.HasPrefix(roleARN, "acs:ram::"+accountID+":role/") || !strings.HasPrefix(providerARN, "acs:ram::"+accountID+":oidc-provider/") || !filepath.IsAbs(tokenPath) {
		return OSSSecrets{}, ErrInvalid
	}
	identity, e := credentials.NewCredential(&credentials.Config{Type: dara.String("oidc_role_arn"), RoleArn: dara.String(roleARN), OIDCProviderArn: dara.String(providerARN), OIDCTokenFilePath: dara.String(tokenPath), RoleSessionName: dara.String("raptor-github-pat"), RoleSessionExpiration: dara.Int(3600), STSEndpoint: dara.String("sts.ap-southeast-1.aliyuncs.com")})
	if e != nil {
		return OSSSecrets{}, ErrUnavailable
	}
	host := bucketName + ".oss-ap-southeast-1.aliyuncs.com"
	client, e := oss.New("https://oss-ap-southeast-1.aliyuncs.com", "", "", oss.SetCredentialsProvider(&oidcCredentials{source: identity}), oss.HTTPClient(&http.Client{Timeout: 30 * time.Second, Transport: fixedOSSTransport{host: host}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if e != nil {
		return OSSSecrets{}, ErrUnavailable
	}
	bucket, e := client.Bucket(bucketName)
	if e != nil {
		return OSSSecrets{}, ErrUnavailable
	}
	if bucket == nil {
		return OSSSecrets{}, errors.New("OSS bucket unavailable")
	}
	return OSSSecrets{Bucket: bucket}, nil
}
