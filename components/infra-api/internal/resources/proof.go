package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"raptor-iap/infra-api/internal/domain"
	"time"
)

// ResolutionIdentity is the immutable tuple for trusted service use.
// Declaration order defines canonical JSON; no capacity, membership or timestamps.
type ResolutionIdentity struct {
	EnvCode            string              `json:"envCode"`
	AccountID          string              `json:"accountId"`
	Region             string              `json:"region"`
	ClusterID          string              `json:"clusterId"`
	ProxyCode          string              `json:"proxyCode"`
	GroupID            string              `json:"groupId"`
	ServerGroupID      string              `json:"serverGroupId"`
	ListenerID         string              `json:"listenerId"`
	Port               int                 `json:"port"`
	TargetDBCode       string              `json:"targetDbCode"`
	DatabaseResourceID string              `json:"databaseResourceId"`
	Application        ApplicationIdentity `json:"application"`
}
type ApplicationIdentity struct {
	EnvCode   string `json:"envCode"`
	AppCode   string `json:"appCode"`
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}
type Proof struct {
	Identity         ResolutionIdentity `json:"identity"`
	ResolutionDigest string             `json:"resolutionDigest"`
}

func Digest(i ResolutionIdentity) (string, error) {
	a := i.Application
	if i.EnvCode == "" || i.AccountID == "" || i.Region != "ap-southeast-1" || i.ClusterID == "" || i.ProxyCode == "" || i.GroupID == "" || i.ServerGroupID == "" || i.ListenerID == "" || i.Port < 1 || i.Port > 65535 || i.TargetDBCode == "" || i.DatabaseResourceID == "" || a.EnvCode != i.EnvCode || a.ClusterID != i.ClusterID || a.AppCode == "" || a.Namespace == "" || a.Name == "" || a.UID == "" {
		return "", domain.ErrIdentity
	}
	b, e := json.Marshal(i)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ResolveProof performs fixed catalog reads only; it never introspects agent tokens.
func (s *Service) ResolveProof(ctx context.Context, env, proxy string) (Proof, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, e := s.resolve(ctx, env, "db-proxy", proxy)
	if e != nil {
		return Proof{}, e
	}
	if len(r.Context.RelatedResources) != 1 || r.Context.ClusterID == "" || r.Proxy == nil || len(r.DatabaseWorkloads) != 1 {
		return Proof{}, domain.ErrScope
	}
	ref := r.Context.RelatedResources[0]
	appContext, e := s.catalogContext(ctx, env, "application", ref.Code)
	if e != nil {
		return Proof{}, e
	}
	if !sameEnvironment(r.Context, appContext) || appContext.Code != ref.Code {
		return Proof{}, domain.ErrIdentity
	}
	// Consume exactly the catalog context just checked; do not re-read it through resolve.
	app, e := s.backend.ResolveResource(ctx, appContext)
	if e != nil {
		return Proof{}, e
	}
	if len(app.ApplicationWorkloads) != 1 {
		return Proof{}, domain.ErrIdentity
	}
	t := app.ApplicationWorkloads[0].Target
	if t.AppCode != ref.Code || t.EnvCode != env || t.ClusterID != r.Context.ClusterID {
		return Proof{}, domain.ErrIdentity
	}
	m := r.Proxy
	i := ResolutionIdentity{EnvCode: env, AccountID: r.Context.AccountID, Region: r.Context.Region, ClusterID: r.Context.ClusterID, ProxyCode: r.Context.Code, GroupID: m.GroupID, ServerGroupID: m.ServerGroupID, ListenerID: m.ListenerID, Port: m.Port, TargetDBCode: m.TargetDBCode, DatabaseResourceID: r.DatabaseWorkloads[0].ResourceID, Application: ApplicationIdentity{EnvCode: t.EnvCode, AppCode: t.AppCode, ClusterID: t.ClusterID, Namespace: t.Namespace, Name: t.Name, UID: t.UID}}
	d, e := Digest(i)
	if e != nil {
		return Proof{}, e
	}
	return Proof{Identity: i, ResolutionDigest: d}, nil
}
