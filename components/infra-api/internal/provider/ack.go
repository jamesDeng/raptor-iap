package provider

import (
	"context"
	"errors"
	cs "github.com/alibabacloud-go/cs-20151215/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"time"
)

type kubeFactory func(context.Context, domain.Environment) (kubernetes.Interface, error)

func cloudKube(config func(string) *openapi.Config) kubeFactory {
	return func(ctx context.Context, env domain.Environment) (kubernetes.Interface, error) {
		c, e := cs.NewClient(config("cs.ap-southeast-1.aliyuncs.com"))
		if e != nil {
			return nil, e
		}
		r, e := c.DescribeClusterUserKubeconfigWithContext(ctx, tea.String(env.ClusterID), &cs.DescribeClusterUserKubeconfigRequest{TemporaryDurationMinutes: tea.Int64(15)}, nil, &dara.RuntimeOptions{})
		if e != nil {
			return nil, e
		}
		if r == nil || r.Body == nil {
			return nil, errors.New("missing cluster configuration")
		}
		expiry, e := time.Parse(time.RFC3339, tea.StringValue(r.Body.Expiration))
		if e != nil || time.Until(expiry) < time.Minute {
			return nil, errors.New("expired cluster configuration")
		}
		raw, e := clientcmd.Load([]byte(tea.StringValue(r.Body.Config)))
		if e != nil {
			return nil, errors.New("invalid cluster configuration")
		}
		for _, a := range raw.AuthInfos {
			if a.Exec != nil || a.AuthProvider != nil || a.TokenFile != "" || a.ClientCertificate != "" || a.ClientKey != "" {
				return nil, errors.New("external credential configuration rejected")
			}
		}
		for _, c := range raw.Clusters {
			u, e := url.Parse(c.Server)
			if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || c.InsecureSkipTLSVerify || c.CertificateAuthority != "" || len(c.CertificateAuthorityData) == 0 || c.ProxyURL != "" {
				return nil, errors.New("unsafe cluster transport")
			}
		}
		rc, e := clientcmd.NewNonInteractiveClientConfig(*raw, raw.CurrentContext, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
		if e != nil {
			return nil, errors.New("invalid cluster configuration")
		}
		rc.Timeout = 10 * time.Second
		return kubernetes.NewForConfig(rc)
	}
}
