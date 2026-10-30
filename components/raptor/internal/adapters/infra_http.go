package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPInfra struct {
	Username, Password, AuthHeader string
	Client                         *http.Client
	ResolveEnvironment             func(context.Context, string) (domain.Environment, error)
}

func (p HTTPInfra) read(ctx context.Context, env domain.Environment, path string, q url.Values, out any) error {
	return p.readRepresentation(ctx, env, path, q, "application/json", out)
}
func (p HTTPInfra) readRepresentation(ctx context.Context, env domain.Environment, path string, q url.Values, accept string, out any) error {
	if p.Username == "" || p.Password == "" || strings.Contains(p.Username, ":") {
		return domain.ErrUnavailable
	}
	header := p.AuthHeader
	if header == "" {
		header = "Authorization"
	}
	if header != "Authorization" && header != "X-Infra-Authorization" {
		return domain.ErrUnavailable
	}
	raw, _ := env.Config["infraApiUrl"].(string)
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return domain.ErrUnavailable
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return domain.ErrUnavailable
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = q.Encode()
	request, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return domain.ErrUnavailable
	}
	request.Header.Set(header, "Basic "+base64.StdEncoding.EncodeToString([]byte(p.Username+":"+p.Password)))
	request.Header.Set("Accept", accept)
	client := http.Client{Timeout: 30 * time.Second}
	if p.Client != nil {
		client = *p.Client
	}
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, e := client.Do(request)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return domain.ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
	if e != nil || len(b) > 2*1024*1024 {
		return domain.ErrUnavailable
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(b, &envelope) != nil || len(envelope) != 1 || envelope["data"] == nil || string(envelope["data"]) == "null" {
		return domain.ErrUnavailable
	}
	if json.Unmarshal(envelope["data"], out) != nil {
		return domain.ErrUnavailable
	}
	return nil
}

type resourceWorkload struct {
	Type         string `json:"type"`
	WorkloadKey  string `json:"workloadKey"`
	Code         string `json:"code"`
	ResourceID   string `json:"resourceId"`
	ClusterID    string `json:"clusterId"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	UID          string `json:"uid"`
	State        string `json:"state"`
	Endpoint     string `json:"endpoint"`
	TargetDBCode string `json:"targetDbCode"`
}
type workloadEnvelope struct {
	Resource struct {
		EnvCode string `json:"envCode"`
		Kind    string `json:"kind"`
		Code    string `json:"code"`
	} `json:"resource"`
	ApplicationWorkloads *[]resourceWorkload `json:"applicationWorkloads"`
	DatabaseWorkloads    *[]resourceWorkload `json:"databaseWorkloads"`
	PgproxyWorkload      *resourceWorkload   `json:"pgproxyWorkload"`
}

func environmentCluster(env domain.Environment) string {
	legacy, _ := env.Config["clusterId"].(string)
	if env.ACKClusterID != "" && legacy != "" && env.ACKClusterID != legacy {
		return ""
	}
	if env.ACKClusterID != "" {
		return env.ACKClusterID
	}
	return legacy
}
func (p HTTPInfra) ListDeployments(ctx context.Context, env domain.Environment, o domain.Object) ([]Deployment, error) {
	var data workloadEnvelope
	if e := p.read(ctx, env, "/v1/deployments", url.Values{"envCode": {env.Code}, "kind": {o.Kind}, "code": {o.Code}}, &data); e != nil {
		return nil, e
	}
	if data.Resource.EnvCode != env.Code || data.Resource.Kind != o.Kind || data.Resource.Code != o.Code {
		return nil, domain.ErrUnavailable
	}
	var workloads []resourceWorkload
	switch o.Kind {
	case "application":
		if data.ApplicationWorkloads == nil || data.DatabaseWorkloads != nil || data.PgproxyWorkload != nil {
			return nil, domain.ErrUnavailable
		}
		workloads = *data.ApplicationWorkloads
	case "database":
		if data.DatabaseWorkloads == nil || data.ApplicationWorkloads != nil || data.PgproxyWorkload != nil {
			return nil, domain.ErrUnavailable
		}
		workloads = *data.DatabaseWorkloads
	case "db-proxy":
		if data.PgproxyWorkload == nil || data.ApplicationWorkloads != nil || data.DatabaseWorkloads != nil {
			return nil, domain.ErrUnavailable
		}
		workloads = []resourceWorkload{*data.PgproxyWorkload}
	default:
		return nil, domain.ErrUnavailable
	}
	rows := make([]Deployment, 0, len(workloads))
	keys := map[string]bool{}
	for _, w := range workloads {
		row := Deployment{WorkloadKey: w.WorkloadKey, ResourceID: w.ResourceID, Kind: o.Kind, EnvCode: env.Code, ObjectCode: o.Code, ClusterID: w.ClusterID, Namespace: w.Namespace, Name: w.Name, UID: w.UID, State: w.State, Endpoint: w.Endpoint, TargetDBCode: w.TargetDBCode, EvidenceMode: "live"}
		switch o.Kind {
		case "application":
			if w.Type != "kubernetes" || environmentCluster(env) == "" || w.ClusterID != environmentCluster(env) || w.WorkloadKey == "" || w.Namespace == "" || w.Name == "" || w.UID == "" || keys[w.WorkloadKey] {
				return nil, domain.ErrUnavailable
			}
			keys[w.WorkloadKey] = true
			row.ResourceID = w.UID
		case "database":
			if w.Type != "rds" || w.Code != o.Code || w.ResourceID == "" || keys[w.ResourceID] {
				return nil, domain.ErrUnavailable
			}
			keys[w.ResourceID] = true
		case "db-proxy":
			if w.Type != "ess" || w.Code != o.Code || w.WorkloadKey == "" || w.TargetDBCode == "" {
				return nil, domain.ErrUnavailable
			}
			row.ResourceID = ""
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func (p HTTPInfra) ResolutionProof(ctx context.Context, envCode, proxyCode string) (ResolutionProof, error) {
	var out ResolutionProof
	if p.ResolveEnvironment == nil {
		return out, domain.ErrUnavailable
	}
	env, e := p.ResolveEnvironment(ctx, envCode)
	if e != nil || env.Code != envCode {
		return out, domain.ErrUnavailable
	}
	if e = p.readRepresentation(ctx, env, "/v1/deployments", url.Values{"envCode": {envCode}, "kind": {"db-proxy"}, "code": {proxyCode}}, "application/vnd.raptor.resolution-proof+json", &out); e != nil {
		return ResolutionProof{}, e
	}
	i := out.Identity
	if i.EnvCode != envCode || i.ProxyCode != proxyCode || i.ClusterID != environmentCluster(env) || i.AccountID != env.AccountID || i.Region != env.Region || i.Digest() == "" || i.Digest() != out.ResolutionDigest {
		return ResolutionProof{}, domain.ErrUnavailable
	}
	return out, nil
}
func (p HTTPInfra) selectedWorkload(ctx context.Context, env domain.Environment, t domain.RestartTarget) (resourceWorkload, error) {
	rows, e := p.ListDeployments(ctx, env, domain.Object{Kind: "application", Code: t.AppCode})
	if e != nil {
		return resourceWorkload{}, e
	}
	var found resourceWorkload
	matches := 0
	for _, r := range rows {
		if r.EnvCode == t.EnvCode && r.ObjectCode == t.AppCode && r.ClusterID == t.ClusterID && r.Namespace == t.Namespace && r.Name == t.Name && r.UID == t.UID {
			found = resourceWorkload{WorkloadKey: r.WorkloadKey, ClusterID: r.ClusterID, Namespace: r.Namespace, Name: r.Name, UID: r.UID}
			matches++
		}
	}
	if matches != 1 {
		return resourceWorkload{}, domain.ErrUnavailable
	}
	return found, nil
}
func (p HTTPInfra) GetDeploymentStatus(ctx context.Context, t domain.RestartTarget) (DeploymentStatus, error) {
	if p.ResolveEnvironment == nil {
		return DeploymentStatus{}, domain.ErrUnavailable
	}
	env, e := p.ResolveEnvironment(ctx, t.EnvCode)
	if e != nil || env.Code != t.EnvCode {
		return DeploymentStatus{}, domain.ErrUnavailable
	}
	if environmentCluster(env) != t.ClusterID || t.ClusterID == "" {
		return DeploymentStatus{}, domain.ErrUnavailable
	}
	selected, e := p.selectedWorkload(ctx, env, t)
	if e != nil {
		return DeploymentStatus{}, e
	}
	var result struct {
		Resource struct {
			EnvCode string `json:"envCode"`
			Kind    string `json:"kind"`
			Code    string `json:"code"`
		} `json:"resource"`
		ApplicationWorkload resourceWorkload `json:"applicationWorkload"`
		Status              DeploymentStatus `json:"status"`
	}
	e = p.read(ctx, env, "/v1/deployment-status", url.Values{"envCode": {t.EnvCode}, "appCode": {t.AppCode}, "workloadKey": {selected.WorkloadKey}}, &result)
	st := result.Status
	w := result.ApplicationWorkload
	if e != nil || result.Resource.EnvCode != t.EnvCode || result.Resource.Kind != "application" || result.Resource.Code != t.AppCode || w.Type != "kubernetes" || w.WorkloadKey != selected.WorkloadKey || w.ClusterID != t.ClusterID || w.Namespace != t.Namespace || w.Name != t.Name || w.UID != t.UID || st.UID != t.UID || st.EvidenceMode != "live" {
		return DeploymentStatus{}, domain.ErrUnavailable
	}

	return st, nil
}

// A mutation is sent once. Missing or mismatched acknowledgement stays unknown.
func (p HTTPInfra) RestartDeployment(ctx context.Context, requestID string, t domain.RestartTarget) error {
	if requestID == "" || t.AppCode == "" || t.EnvCode == "" || t.ClusterID == "" || t.Namespace == "" || t.Name == "" || t.UID == "" || p.ResolveEnvironment == nil {
		return ErrRejected
	}
	env, e := p.ResolveEnvironment(ctx, t.EnvCode)
	if e != nil || env.Code != t.EnvCode || environmentCluster(env) != t.ClusterID {
		return ErrRejected
	}
	selected, e := p.selectedWorkload(ctx, env, t)
	if e != nil {
		return ErrRejected
	}
	type command struct {
		RequestID   string `json:"requestId"`
		EnvCode     string `json:"envCode"`
		AppCode     string `json:"appCode"`
		WorkloadKey string `json:"workloadKey"`
	}
	input := command{requestID, t.EnvCode, t.AppCode, selected.WorkloadKey}
	type receiptTarget struct {
		RequestID string `json:"requestId"`
		domain.RestartTarget
	}
	expected := receiptTarget{requestID, t}
	body, e := json.Marshal(input)
	if e != nil {
		return ErrRejected
	}
	var receipt struct {
		Accepted     bool          `json:"accepted"`
		Target       receiptTarget `json:"target"`
		Generation   int64         `json:"generation"`
		EvidenceMode string        `json:"evidenceMode"`
	}
	if e = p.submitRestart(ctx, env, body, &receipt); e != nil {
		return e
	}
	if !receipt.Accepted || receipt.Target != expected || receipt.Generation <= 0 || receipt.EvidenceMode != "live" {
		return ErrSubmissionUnknown
	}
	return nil
}
func (p HTTPInfra) submitRestart(ctx context.Context, env domain.Environment, body []byte, out any) error {
	if p.Username == "" || p.Password == "" || strings.Contains(p.Username, ":") {
		return ErrRejected
	}
	header := p.AuthHeader
	if header == "" {
		header = "Authorization"
	}
	if header != "Authorization" && header != "X-Infra-Authorization" {
		return ErrRejected
	}
	raw, _ := env.Config["infraApiUrl"].(string)
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ErrRejected
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return ErrRejected
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/deployment-restart"

	request, e := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if e != nil {
		return ErrRejected
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(header, "Basic "+base64.StdEncoding.EncodeToString([]byte(p.Username+":"+p.Password)))
	client := http.Client{Timeout: 30 * time.Second}
	if p.Client != nil {
		client = *p.Client
	}
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, e := client.Do(request)
	if e != nil {
		return ErrSubmissionUnknown
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		switch r.StatusCode {
		case 400, 401, 403, 404, 409, 422:
			return ErrRejected
		}
		return ErrSubmissionUnknown
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
	if e != nil || len(b) > 2*1024*1024 {
		return ErrSubmissionUnknown
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(b, &envelope) != nil || len(envelope) != 1 || envelope["data"] == nil || string(envelope["data"]) == "null" {
		return ErrSubmissionUnknown
	}
	if json.Unmarshal(envelope["data"], out) != nil {
		return ErrSubmissionUnknown
	}
	return nil
}
