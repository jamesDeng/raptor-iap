package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net"
	"net/url"
	"regexp"
	"time"
)

type ObserverSQLCredential struct{ User, Password, Database, SSLMode string }

func (ObserverSQLCredential) String() string     { return "[observer SQL credential]" }
func (c ObserverSQLCredential) GoString() string { return c.String() }

// ObserverPreparation composes controller-owned discoveries and credential
// issuers. None of its endpoints or SQL identity originate from agent input.
type ObserverPreparation struct {
	Server                                  string
	CA                                      []byte
	DBInstanceID, ListenerID, PrometheusURL string
	ClientTargets                           []string
	Cloud                                   func(context.Context, execution.LiveStart) (ObserverCredential, error)
	Kube                                    func(context.Context, execution.LiveStart) (ObserverKubeCredential, error)
	SQL                                     func(context.Context, execution.LiveStart) (ObserverSQLCredential, error)
}

func (p ObserverPreparation) Prepare(ctx context.Context, in execution.LiveStart) (ObservationMaterial, error) {
	if ctx.Err() != nil || in.Access.ReplacementScope == nil || !in.Access.ReplacementScope.ValidFor(in.Binding) || p.Cloud == nil || p.Kube == nil || p.SQL == nil || len(p.CA) == 0 || len(p.CA) > 16384 || p.DBInstanceID == "" || p.ListenerID == "" || len(p.ClientTargets) == 0 || len(p.ClientTargets) > 10 {
		return ObservationMaterial{}, ErrConfiguration
	}
	endpoint, err := url.Parse(p.PrometheusURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || !net.ParseIP(endpoint.Hostname()).IsPrivate() {
		return ObservationMaterial{}, ErrConfiguration
	}
	for _, target := range p.ClientTargets {
		host, port, err := net.SplitHostPort(target)
		if err != nil || port != "9090" || !net.ParseIP(host).IsPrivate() {
			return ObservationMaterial{}, ErrConfiguration
		}
	}
	scope := *in.Access.ReplacementScope
	cloud, err := p.Cloud(ctx, in)
	if err != nil {
		return ObservationMaterial{}, ErrRuntimeUnavailable
	}
	kube, err := p.Kube(ctx, in)
	if err != nil {
		return ObservationMaterial{}, ErrRuntimeUnavailable
	}
	sql, err := p.SQL(ctx, in)
	if err != nil {
		return ObservationMaterial{}, ErrRuntimeUnavailable
	}
	until := cloud.ExpiresAt
	if kube.ExpiresAt.Before(until) {
		until = kube.ExpiresAt
	}
	if ctx.Err() != nil || until.Before(time.Now().Add(120*time.Second)) || until.After(time.Now().Add(930*time.Second)) || cloud.AccessKeyID == "" || cloud.AccessKeySecret == "" || cloud.SecurityToken == "" || kube.Token == "" || sql.Password == "" || len(sql.Password) > 4096 || !regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`).MatchString(sql.User) || !regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`).MatchString(sql.Database) || (sql.SSLMode != "disable" && sql.SSLMode != "verify-full") {
		return ObservationMaterial{}, ErrRuntimeUnavailable
	}
	app := scope.Application
	config := map[string]any{"version": 1, "requestId": in.Binding.RequestID, "definitionSha256": in.Binding.DefinitionSHA256, "envCode": in.Binding.EnvCode, "accountId": "1360282071200743", "region": "ap-southeast-1", "groupId": scope.GroupID, "serverGroupId": scope.ServerGroupID, "dbInstanceId": p.DBInstanceID, "listenerId": p.ListenerID, "application": map[string]any{"clusterId": app.ClusterID, "namespace": app.Namespace, "name": app.Name, "uid": app.UID}, "clientTargets": append([]string(nil), p.ClientTargets...), "prometheusURL": p.PrometheusURL, "expiresAt": until, "private": map[string]any{"kubeconfig": "/tmp/raptor-private/kubeconfig", "aliyunEnv": map[string]string{"ALIBABA_CLOUD_ACCESS_KEY_ID": cloud.AccessKeyID, "ALIBABA_CLOUD_ACCESS_KEY_SECRET": cloud.AccessKeySecret, "ALIBABA_CLOUD_SECURITY_TOKEN": cloud.SecurityToken}, "sql": map[string]string{"user": sql.User, "password": sql.Password, "database": sql.Database, "sslmode": sql.SSLMode}}}
	kubeconfig := map[string]any{"apiVersion": "v1", "kind": "Config", "current-context": "observer", "clusters": []any{map[string]any{"name": app.ClusterID, "cluster": map[string]any{"server": p.Server, "certificate-authority-data": base64.StdEncoding.EncodeToString(p.CA)}}}, "contexts": []any{map[string]any{"name": "observer", "context": map[string]any{"cluster": app.ClusterID, "user": "observer", "namespace": app.Namespace}}}, "users": []any{map[string]any{"name": "observer", "user": map[string]any{"token": kube.Token}}}}
	raw, err := json.Marshal(config)
	if err != nil {
		return ObservationMaterial{}, ErrConfiguration
	}
	kraw, err := json.Marshal(kubeconfig)
	if err != nil {
		return ObservationMaterial{}, ErrConfiguration
	}
	material := ObservationMaterial{Config: raw, Kubeconfig: kraw, Secrets: []string{cloud.AccessKeyID, cloud.AccessKeySecret, cloud.SecurityToken, kube.Token, sql.Password}}
	if material.Validate(in.Binding, scope) != nil {
		return ObservationMaterial{}, ErrConfiguration
	}
	return material, nil
}
