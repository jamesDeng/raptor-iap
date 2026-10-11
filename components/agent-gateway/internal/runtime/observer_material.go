package runtime

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"time"
)

type ObservationMaterial struct {
	Config, Kubeconfig []byte
	Secrets            []string
}

func (ObservationMaterial) String() string     { return "[private observation material]" }
func (m ObservationMaterial) GoString() string { return m.String() }

type ObservationPreparer func(context.Context, execution.LiveStart) (ObservationMaterial, error)

func (m ObservationMaterial) Validate(b execution.AttemptBinding, s execution.ReplacementScope) error {
	if len(m.Config) == 0 || len(m.Config) > 65536 || len(m.Kubeconfig) == 0 || len(m.Kubeconfig) > 65536 {
		return ErrConfiguration
	}
	var c struct {
		Version          int       `json:"version"`
		RequestID        string    `json:"requestId"`
		DefinitionSHA256 string    `json:"definitionSha256"`
		EnvCode          string    `json:"envCode"`
		AccountID        string    `json:"accountId"`
		Region           string    `json:"region"`
		GroupID          string    `json:"groupId"`
		ServerGroupID    string    `json:"serverGroupId"`
		ExpiresAt        time.Time `json:"expiresAt"`
		Application      struct {
			ClusterID            string `json:"clusterId"`
			Namespace, Name, UID string
		} `json:"application"`
		Private struct {
			Kubeconfig string `json:"kubeconfig"`
		} `json:"private"`
	}
	if json.Unmarshal(m.Config, &c) != nil || c.Version != 1 || c.RequestID != b.RequestID || c.DefinitionSHA256 != b.DefinitionSHA256 || c.EnvCode != b.EnvCode || c.AccountID != "1360282071200743" || c.Region != "ap-southeast-1" || c.GroupID != s.GroupID || c.ServerGroupID != s.ServerGroupID || c.Application.ClusterID != b.ClusterID || c.Application.Namespace != s.Application.Namespace || c.Application.Name != s.Application.Name || c.Application.UID != s.Application.UID || c.Private.Kubeconfig != "/tmp/raptor-private/kubeconfig" || c.ExpiresAt.Before(time.Now().Add(120*time.Second)) || c.ExpiresAt.After(time.Now().Add(930*time.Second)) {
		return ErrConfiguration
	}
	return validateObserverKubeconfig(m.Kubeconfig, b, s, c.ExpiresAt)
}

func (n *NativeLive) prepareObservation(ctx context.Context, in execution.LiveStart) (ObservationMaterial, error) {
	if in.Binding.Operation != "db-proxy.replace-nodes" {
		return ObservationMaterial{}, nil
	}
	if n.Observe == nil || in.Access.ReplacementScope == nil || !in.Access.ReplacementScope.ValidFor(in.Binding) {
		return ObservationMaterial{}, ErrConfiguration
	}
	material, err := n.Observe(ctx, in)
	if err != nil || material.Validate(in.Binding, *in.Access.ReplacementScope) != nil {
		return ObservationMaterial{}, ErrConfiguration
	}
	return material, nil
}
func observationConfigPath(in execution.LiveStart) string {
	if in.Binding.Operation == "db-proxy.replace-nodes" {
		return "/tmp/raptor-private/observation.json"
	}
	return ""
}
