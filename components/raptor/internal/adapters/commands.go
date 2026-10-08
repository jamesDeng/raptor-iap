package adapters

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

var ErrSubmissionUnknown = errors.New("SubmissionUnknown")
var ErrRejected = errors.New("Rejected")

type DeploymentStatus struct {
	UID                string `json:"uid"`
	Generation         int64  `json:"generation"`
	ObservedGeneration int64  `json:"observedGeneration"`
	Replicas           int    `json:"replicas"`
	DesiredReplicas    int    `json:"desiredReplicas"`
	AvailableReplicas  int    `json:"availableReplicas"`
	UpdatedReplicas    int    `json:"updatedReplicas"`
	ReadyReplicas      int    `json:"readyReplicas"`
	EvidenceMode       string `json:"evidenceMode"`
}
type InfraCommands interface {
	RestartDeployment(context.Context, string, domain.RestartTarget) error
	GetDeploymentStatus(context.Context, domain.RestartTarget) (DeploymentStatus, error)
}
