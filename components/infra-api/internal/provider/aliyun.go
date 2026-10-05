package provider

import (
	"context"
	"errors"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	sts "github.com/alibabacloud-go/sts-20150401/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"
	"raptor-iap/infra-api/internal/api"
	"raptor-iap/infra-api/internal/domain"
)

type reader struct {
	identity func(context.Context, domain.Environment) (string, error)
	page     func(context.Context, domain.Environment, string, string, int) (page, error)
	kube     kubeFactory
	evidence string
}

func New() (api.Reader, error) {
	cred, e := credentials.NewCredential(nil)
	if e != nil {
		return nil, errors.New("credential provider unavailable")
	}
	config := func(endpoint string) *openapi.Config {
		return &openapi.Config{Credential: cred, Endpoint: tea.String(endpoint), ConnectTimeout: tea.Int(5000), ReadTimeout: tea.Int(10000)}
	}
	p := &reader{evidence: "live"}
	p.identity = func(ctx context.Context, env domain.Environment) (string, error) {
		c, e := sts.NewClient(config("sts.ap-southeast-1.aliyuncs.com"))
		if e != nil {
			return "", e
		}
		r, e := c.GetCallerIdentity()
		if e != nil {
			return "", e
		}
		if r == nil || r.Body == nil {
			return "", errors.New("missing identity")
		}
		return tea.StringValue(r.Body.AccountId), nil
	}
	p.page = cloudPages(config)
	p.kube = cloudKube(config)
	return p, nil
}
func (p *reader) Identity(ctx context.Context, env domain.Environment) (string, error) {
	if p.identity == nil {
		return "", domain.ErrNotConfigured
	}
	return p.identity(ctx, env)
}
func (p *reader) Deployments(ctx context.Context, env domain.Environment, kind, code string) ([]domain.Deployment, error) {
	if kind == "application" {
		return p.applications(ctx, env, code)
	}
	id, e := p.Identity(ctx, env)
	if e != nil {
		return nil, e
	}
	if id != env.AccountID {
		return nil, domain.ErrScope
	}
	all, e := p.discover(ctx, env, kind, code)
	if e != nil {
		return nil, e
	}
	out := []domain.Deployment{}
	key := "db-code"
	if kind == "db-proxy" {
		key = "db-proxy-code"
	}
	for _, r := range all {
		if r.Tags["env"] == env.Code && r.Tags[key] == code {
			out = append(out, project(r, env, kind, code, p.evidence))
		}
	}
	return out, nil
}
