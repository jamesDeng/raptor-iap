package adapters

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"os"
)

type UnavailableInfra struct{}

func (UnavailableInfra) ListDeployments(context.Context, domain.Environment, domain.Object) ([]Deployment, error) {
	return nil, domain.ErrUnavailable
}

type FixtureInfra struct{ Path string }

func (f FixtureInfra) ListDeployments(ctx context.Context, e domain.Environment, o domain.Object) ([]Deployment, error) {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, domain.ErrUnavailable
	}
	var fixture struct {
		Deployments []Deployment `json:"deployments"`
	}
	if json.Unmarshal(b, &fixture) != nil {
		return nil, domain.ErrUnavailable
	}
	out := []Deployment{}
	for _, d := range fixture.Deployments {
		if d.EnvCode == e.Code && d.ObjectCode == o.Code && d.Kind == o.Kind {
			d.EvidenceMode = "simulated"
			out = append(out, d)
		}
	}
	return out, nil
}
