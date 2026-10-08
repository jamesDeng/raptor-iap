package api

import (
	"context"
	"errors"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"time"
)

type operationError struct {
	status int
	code   string
}

func (e *operationError) Error() string { return e.code }

type operations struct {
	reader     Reader
	commander  Commander
	authorizer Authorizer
	principal  string
	scopes     map[string]domain.Environment
}

func (o operations) execute(ctx context.Context, path string, q url.Values) (any, error) {
	var allowed []string
	switch path {
	case "/v1/cloud/identity":
		allowed = []string{"envCode"}
	case "/v1/deployments":
		allowed = []string{"envCode", "kind", "code"}
	case "/v1/deployment-status":
		allowed = []string{"envCode", "appCode", "clusterId", "namespace", "name", "uid"}
	default:
		return nil, &operationError{404, "NotFound"}
	}

	for k, v := range q {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
			}
		}
		if !found || len(v) != 1 || len(v[0]) == 0 || len(v[0]) > 256 {
			return nil, &operationError{400, "InvalidInput"}
		}
	}
	for _, a := range allowed {
		if q.Get(a) == "" {
			return nil, &operationError{400, "InvalidInput"}
		}
	}
	env, ok := o.scopes[q.Get("envCode")]
	if !ok {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if o.authorizer != nil && o.authorizer.Authorize(ctx, o.principal, path, q) != nil {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var data any
	var e error
	switch path {
	case "/v1/cloud/identity":
		var account string
		account, e = o.reader.Identity(ctx, env)
		if e == nil && account != env.AccountID {
			e = domain.ErrScope
		}
		data = map[string]any{"envCode": env.Code, "accountMatches": true, "evidenceMode": "live", "observedAt": time.Now().UTC()}
	case "/v1/deployments":
		kind := q.Get("kind")
		if kind != "application" && kind != "database" && kind != "db-proxy" {
			return nil, &operationError{400, "InvalidInput"}
		}
		var rows []domain.Deployment
		rows, e = o.reader.Deployments(ctx, env, kind, q.Get("code"))
		if rows == nil {
			rows = []domain.Deployment{}
		}

		if e == nil {
			now := time.Now().UTC()
			for i := range rows {
				if rows[i].EnvCode != env.Code || rows[i].ObjectCode != q.Get("code") {
					e = domain.ErrScope
					break
				}
				rows[i].ObservedAt = now
			}
		}
		data = rows
	case "/v1/deployment-status":
		target := domain.Target{EnvCode: env.Code, AppCode: q.Get("appCode"), ClusterID: q.Get("clusterId"), Namespace: q.Get("namespace"), Name: q.Get("name"), UID: q.Get("uid")}
		var status domain.Status
		status, e = o.reader.Status(ctx, env, target)
		if e == nil {
			if status.UID != target.UID {
				e = domain.ErrIdentity
			} else {
				status.EnvCode = target.EnvCode
				status.ObjectCode = target.AppCode
				status.ClusterID = target.ClusterID
				status.Namespace = target.Namespace
				status.Name = target.Name
				status.ObservedAt = time.Now().UTC()
			}
		}
		data = status
	}
	if e != nil {
		switch {
		case errors.Is(e, domain.ErrScope):
			return nil, &operationError{403, "ScopeMismatch"}
		case errors.Is(e, domain.ErrIdentity):
			return nil, &operationError{409, "IdentityChanged"}
		case errors.Is(e, domain.ErrNotConfigured):
			return nil, &operationError{503, "NotConfigured"}
		default:
			return nil, &operationError{503, "ProviderUnavailable"}
		}
	}
	return data, nil
}
