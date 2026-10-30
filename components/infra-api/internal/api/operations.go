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

// These are logical MCP reads, never HTTP routes or arbitrary provider queries.
func proxyReadOptions(tool string) (string, []string) {
	switch tool {
	case "proxy_observation_get":
		return "kind", []string{"fleet", "backend", "capacity"}
	case "proxy_pgcat_metrics_get":
		return "series", []string{"up", "upTimestamp", "active", "activeTimestamp", "idle", "idleTimestamp", "waiting", "waitingTimestamp"}
	}
	return "", nil
}

// Validate the entire raw payload before removing the finite selection from the
// identity selectors sent to Raptor. Extra fields must not disappear here.
func proxyReadSelectors(tool string, args map[string]string) (map[string]string, error) {
	field, options := proxyReadOptions(tool)
	if field == "" || len(args) != 6 {
		return nil, domain.ErrScope
	}
	selectors := map[string]string{}
	for _, key := range []string{"requestId", "envCode", "proxyCode", "groupId", "serverGroupId"} {
		if !textOK(args[key]) {
			return nil, domain.ErrScope
		}
		selectors[key] = args[key]
	}
	for _, option := range options {
		if args[field] == option {
			return selectors, nil
		}
	}
	return nil, domain.ErrScope
}

func (o operations) proxyRead(ctx context.Context, tool string, args map[string]string) (any, error) {
	if _, err := proxyReadSelectors(tool, args); err != nil {
		return nil, &operationError{400, "InvalidInput"}
	}
	env, ok := o.scopes[args["envCode"]]
	if !ok || env.Code != args["envCode"] {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if o.authorizer != nil && o.authorizer.Authorize(ctx, o.principal, tool, args) != nil {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	var mapping domain.ProxyMapping
	matches := 0
	for _, candidate := range env.Proxies {
		if candidate.Code == args["proxyCode"] {
			mapping = candidate
			matches++
		}
	}
	if matches != 1 || mapping.GroupID != args["groupId"] || mapping.ServerGroupID != args["serverGroupId"] {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if o.principal == "agent" {
		// Authorize has just re-introspected and required both values to equal this
		// private initial identity. Reuse that identity; do not add another check/cache.
		auth, ok := o.authorizer.(agentAuthorizer)
		if !ok || auth.identity.Binding == nil || auth.identity.ReplacementScope == nil {
			return nil, &operationError{403, "ScopeMismatch"}
		}
		b, s := auth.identity.Binding, auth.identity.ReplacementScope
		if env.ClusterID != b.ClusterID || env.ClusterID != s.Application.ClusterID || mapping.TargetDBCode != s.TargetDBCode || env.Code != s.EnvCode || mapping.Code != s.ProxyCode || mapping.GroupID != s.GroupID || mapping.ServerGroupID != s.ServerGroupID {
			return nil, &operationError{403, "ScopeMismatch"}
		}
	}
	observer, ok := o.reader.(ProxyObserver)
	if !ok {
		return nil, &operationError{503, "NotConfigured"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	target := domain.ProxyReadTarget{RequestID: args["requestId"], EnvCode: env.Code, ProxyCode: mapping.Code, GroupID: mapping.GroupID, ServerGroupID: mapping.ServerGroupID}
	var data any
	var err error
	if tool == "proxy_pgcat_metrics_get" {
		data, err = observer.ObservePgcatSeries(ctx, env, target, args["series"])
	} else {
		var observation domain.ProxyObservation
		observation, err = observer.ObserveProxy(ctx, env, target, args["kind"])
		if err == nil {
			branches := 0
			if observation.Fleet != nil {
				branches++
			}
			if observation.Backend != nil {
				branches++
			}
			if observation.Capacity != nil {
				branches++
			}
			switch args["kind"] {
			case "fleet":
				if observation.Fleet != nil {
					data = observation.Fleet
				}
			case "backend":
				if observation.Backend != nil {
					data = observation.Backend
				}
			case "capacity":
				if observation.Capacity != nil {
					data = observation.Capacity
				}
			}
			if branches != 1 || data == nil {
				return nil, &operationError{503, "ProviderUnavailable"}
			}
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrScope):
			return nil, &operationError{403, "ScopeMismatch"}
		case errors.Is(err, domain.ErrIdentity):
			return nil, &operationError{409, "IdentityChanged"}
		case errors.Is(err, domain.ErrNotConfigured):
			return nil, &operationError{503, "NotConfigured"}
		default:
			return nil, &operationError{503, "ProviderUnavailable"}
		}
	}
	return data, nil
}
