package api

import (
	"context"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"regexp"
	"time"
)

type resourceOperations struct {
	reader                Reader
	resolver              ResourceResolver
	commander             Commander
	authorizer            Authorizer
	principal, credential string
	check                 AgentCheck
	identity              *AgentIdentity
}
type resourceSelector struct {
	EnvCode string `json:"envCode"`
	Kind    string `json:"kind"`
	Code    string `json:"code"`
}
type resourceEnvelope struct {
	Resource             resourceSelector              `json:"resource"`
	ApplicationWorkloads *[]domain.ApplicationWorkload `json:"applicationWorkloads,omitempty"`
	DatabaseWorkloads    *[]domain.DatabaseWorkload    `json:"databaseWorkloads,omitempty"`
	PgproxyWorkload      *domain.PgproxyWorkload       `json:"pgproxyWorkload,omitempty"`
}

var resourceCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

func resourceInput(q url.Values, required, optional []string) bool {
	if len(q) < len(required) || len(q) > len(required)+len(optional) {
		return false
	}
	allowed := map[string]bool{}
	for _, k := range append(append([]string{}, required...), optional...) {
		allowed[k] = true
	}
	for k, v := range q {
		if !allowed[k] || len(v) != 1 || !textOK(v[0]) {
			return false
		}
	}
	for _, k := range required {
		if q.Get(k) == "" {
			return false
		}
	}
	for _, k := range []string{"envCode", "code", "appCode", "proxyCode"} {
		if v := q.Get(k); v != "" && !resourceCode.MatchString(v) {
			return false
		}
	}
	return true
}
func stringsMap(q url.Values) map[string]string {
	out := map[string]string{}
	for k, v := range q {
		out[k] = v[0]
	}
	return out
}
func (o resourceOperations) authorize(ctx context.Context, path, tool string, selectors map[string]string, args any, mutation bool) (*AgentIdentity, error) {
	if o.principal == "agent" {
		i, e := o.check(ctx, o.credential, tool, selectors)
		if e != nil || !i.permitsResource(tool, selectors) {
			return nil, &operationError{403, "ScopeMismatch"}
		}
		if i.Binding.Operation == "db-proxy.replace-nodes" {
			if _, e := resourceIdentityProof(i); e != nil {
				return nil, e
			}
		}
		if mutation && (o.authorizer == nil || !o.visible(path)) {
			return nil, &operationError{403, "ScopeMismatch"}
		}
		return &i, nil
	}
	if (mutation || path == "/v1/resource-resolution-proof") && o.authorizer == nil {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if o.authorizer != nil && o.authorizer.Authorize(ctx, o.principal, path, args) != nil {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	return nil, nil
}
func (o resourceOperations) visible(path string) bool {
	if o.authorizer == nil {
		return false
	}
	if f, ok := o.authorizer.(interface{ VisibleCommand(string) bool }); ok {
		return f.VisibleCommand(path)
	}
	return true
}
func (o resourceOperations) execute(ctx context.Context, path string, q url.Values) (any, error) {
	var required, optional []string
	tool, kind, code := "", "", ""
	switch path {
	case "/v1/deployments":
		required = []string{"envCode", "kind", "code"}
		tool = "deployments_list"
		kind = q.Get("kind")
		code = q.Get("code")
		if kind != "application" && kind != "database" && kind != "db-proxy" {
			return nil, &operationError{400, "InvalidInput"}
		}
	case "/v1/deployment-status":
		required = []string{"envCode", "appCode"}
		optional = []string{"workloadKey"}
		tool = "deployment_status_get"
		kind = "application"
		code = q.Get("appCode")
	case "/v1/resource-resolution-proof":
		required = []string{"envCode", "proxyCode"}
		kind = "db-proxy"
		code = q.Get("proxyCode")
		if o.principal == "agent" {
			return nil, &operationError{403, "ScopeMismatch"}
		}
	default:
		return nil, &operationError{404, "NotFound"}
	}
	if !resourceInput(q, required, optional) {
		return nil, &operationError{400, "InvalidInput"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	identity, e := o.authorize(ctx, path, tool, stringsMap(q), q, false)
	if e != nil {
		return nil, e
	}
	if path == "/v1/resource-resolution-proof" {
		p, e := o.resolver.ResolveProof(ctx, q.Get("envCode"), code)
		if e != nil {
			return nil, commandFailure(e)
		}
		return p, nil
	}
	r, e := o.resolver.Resolve(ctx, q.Get("envCode"), kind, code)
	if e != nil {
		return nil, commandFailure(e)
	}
	if !resolvedSelectors(r, q.Get("envCode"), kind, code, identity) {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	if identity != nil && identity.ReplacementScope != nil {
		if e := compareResolvedProof(*identity, r); e != nil {
			return nil, e
		}
	}
	root := resourceSelector{r.Context.EnvCode, r.Context.Kind, r.Context.Code}
	if path == "/v1/deployments" {
		result := resourceEnvelope{Resource: root}
		switch kind {
		case "application":
			if identity != nil {
				// Check the environment even when discovery returns no workloads.
				if r.Environment.ClusterID != identity.Binding.ClusterID {
					return nil, &operationError{409, "IdentityChanged"}
				}
				for _, workload := range r.ApplicationWorkloads {
					if e := validateApplicationWorkload(r, workload, identity); e != nil {
						return nil, e
					}
				}
			}
			rows := r.ApplicationWorkloads
			if rows == nil {
				rows = []domain.ApplicationWorkload{}
			}
			result.ApplicationWorkloads = &rows
		case "database":
			rows := r.DatabaseWorkloads
			if rows == nil {
				rows = []domain.DatabaseWorkload{}
			}
			result.DatabaseWorkloads = &rows
		case "db-proxy":
			result.PgproxyWorkload = r.PgproxyWorkload
		}
		return result, nil
	}
	a, e := selectApplication(r, q.Get("workloadKey"), identity)
	if e != nil {
		return nil, e
	}
	status, e := o.reader.Status(ctx, r.Environment, a.Target)
	if e != nil {
		return nil, commandFailure(e)
	}
	if status.UID != a.Target.UID {
		return nil, &operationError{409, "IdentityChanged"}
	}
	// Kubernetes Status performs a fresh provider read but historically leaves its
	// observation time to the API. Preserve an explicit source time when supplied.
	if status.ObservedAt.IsZero() {
		status.ObservedAt = time.Now().UTC()
	}
	status.EnvCode = a.Target.EnvCode
	status.ObjectCode = a.Target.AppCode
	status.ClusterID = a.Target.ClusterID
	status.Namespace = a.Target.Namespace
	status.Name = a.Target.Name
	return struct {
		Resource            resourceSelector           `json:"resource"`
		ApplicationWorkload domain.ApplicationWorkload `json:"applicationWorkload"`
		Status              domain.Status              `json:"status"`
	}{root, a, status}, nil
}
func resolvedSelectors(r domain.ResolvedResource, env, kind, code string, i *AgentIdentity) bool {
	if r.Context.EnvCode != env || r.Context.Kind != kind || r.Context.Code == "" {
		return false
	}
	if i != nil && r.Context.Code != code {
		return false
	}
	return true
}
func selectApplication(r domain.ResolvedResource, key string, i *AgentIdentity) (domain.ApplicationWorkload, error) {
	var found domain.ApplicationWorkload
	count := 0
	for _, a := range r.ApplicationWorkloads {
		if key == "" || a.WorkloadKey == key {
			found = a
			count++
		}
	}
	if count != 1 {
		return found, &operationError{409, "AmbiguousTarget"}
	}
	return found, validateApplicationWorkload(r, found, i)
}

// Discovery and exact workload selection enforce the same private/public identity.
func validateApplicationWorkload(r domain.ResolvedResource, workload domain.ApplicationWorkload, i *AgentIdentity) error {
	t := workload.Target
	if r.Environment.Code != r.Context.EnvCode || t.EnvCode != r.Context.EnvCode || t.AppCode != r.Context.Code || t.ClusterID != r.Environment.ClusterID || !textOK(t.Namespace) || !textOK(t.Name) || !textOK(t.UID) || workload.Type != "kubernetes" || !textOK(workload.WorkloadKey) || workload.ClusterID != t.ClusterID || workload.Namespace != t.Namespace || workload.Name != t.Name || workload.UID != t.UID {
		return &operationError{409, "IdentityChanged"}
	}
	if i != nil && (i.Binding == nil || t.ClusterID != i.Binding.ClusterID) {
		return &operationError{409, "IdentityChanged"}
	}
	if i != nil && i.ReplacementScope != nil {
		a := i.ReplacementScope.Application
		if t.EnvCode != a.EnvCode || t.AppCode != a.AppCode || t.ClusterID != a.ClusterID || t.Namespace != a.Namespace || t.Name != a.Name || t.UID != a.UID {
			return &operationError{409, "IdentityChanged"}
		}
	}
	return nil
}
func (o resourceOperations) proxyRead(ctx context.Context, tool string, args map[string]string) (any, error) {
	field, options := proxyReadOptions(tool)
	q := url.Values{}
	for k, v := range args {
		q[k] = []string{v}
	}
	valid := false
	for _, v := range options {
		if args[field] == v {
			valid = true
		}
	}
	if !valid || !resourceInput(q, []string{"requestId", "envCode", "proxyCode", field}, nil) {
		return nil, &operationError{400, "InvalidInput"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	selectors := map[string]string{"requestId": args["requestId"], "envCode": args["envCode"], "proxyCode": args["proxyCode"]}
	i, e := o.authorize(ctx, tool, tool, selectors, args, false)
	if e != nil {
		return nil, e
	}
	r, e := o.resolver.Resolve(ctx, args["envCode"], "db-proxy", args["proxyCode"])
	if e != nil {
		return nil, commandFailure(e)
	}
	if !resolvedSelectors(r, args["envCode"], "db-proxy", args["proxyCode"], i) || r.Proxy == nil || r.PgproxyWorkload == nil || r.PgproxyWorkload.Type != "ess" {
		return nil, &operationError{409, "IdentityChanged"}
	}
	if i != nil {
		if e = o.matchProof(ctx, *i, r); e != nil {
			return nil, e
		}
	}
	// Reuse the finite branch dispatcher with already authorized exact internal targets.
	internal := map[string]string{"requestId": args["requestId"], "envCode": r.Environment.Code, "proxyCode": r.Proxy.Code, "groupId": r.Proxy.GroupID, "serverGroupId": r.Proxy.ServerGroupID, field: args[field]}
	data, e := (operations{reader: o.reader, scopes: map[string]domain.Environment{r.Environment.Code: r.Environment}}).proxyRead(ctx, tool, internal)
	if e != nil {
		return nil, e
	}
	workload := map[string]any{"type": "ess", "code": r.PgproxyWorkload.Code, "workloadKey": r.PgproxyWorkload.WorkloadKey, "state": r.PgproxyWorkload.State, "targetDbCode": r.PgproxyWorkload.TargetDBCode}
	var observed time.Time
	switch v := data.(type) {
	case domain.PgcatSeriesObservation:
		observed = v.ObservedAt
		workload["metrics"] = v
	case *domain.FleetObservation:
		observed = v.ObservedAt
		workload["fleet"] = v
	case *domain.BackendObservation:
		observed = v.ObservedAt
		workload["backend"] = v
	case *domain.CapacityObservation:
		observed = v.ObservedAt
		workload["capacity"] = v
	}
	return map[string]any{"envCode": r.Context.EnvCode, "proxyCode": r.Context.Code, "observedAt": observed, "pgproxyWorkload": workload}, nil
}
