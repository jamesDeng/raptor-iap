package api

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"raptor-iap/infra-api/internal/resources"
)

const requestGrantHeader = "X-Raptor-Request-Grant"

var grantIDPattern = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)

type RequestGrantClient interface {
	ClaimGrant(context.Context, approval.GrantAction, resources.Proof) (approval.GrantReceipt, error)
	RecordGrant(context.Context, approval.GrantAction, string, string) error
}

type pgcatInventoryWriter interface {
	SyncPgcatInventory(context.Context, domain.Environment, domain.ProxyReadTarget) (domain.PgcatInventoryReceipt, error)
}

func (o resourceOperations) syncMonitoring(ctx context.Context, identity AgentIdentity, resolved domain.ResolvedResource, cmd resourceInventory, raw json.RawMessage) (any, error) {
	writer, ok := o.reader.(pgcatInventoryWriter)
	if !ok || resolved.Proxy == nil || resolved.Proxy.Code != cmd.ProxyCode {
		return nil, &operationError{503, "NotConfigured"}
	}
	proof, err := resourceIdentityProof(identity)
	if err != nil {
		return nil, err
	}
	toolPolicy, ok := policy.ApprovalPolicyForTool("pgcat_monitoring_sync", identity.Binding.ApprovalProtocol)
	if !ok || len(toolPolicy.Variants) != 1 || !toolPolicy.Variants[0].RequiresHumanApproval {
		return nil, &operationError{403, "ApprovalRequired"}
	}
	variant := toolPolicy.Variants[0]
	action := approval.GrantAction{RequestID: cmd.RequestID, GrantID: o.grantID, ActionID: cmd.ActionID,
		EnvCode: cmd.EnvCode, ScopeDigest: proof.ResolutionDigest, Arguments: raw,
		Capability: approval.GrantCapability{Capability: variant.Capability,
			Target:      approval.GrantTarget{Kind: variant.TargetKind, Code: cmd.ProxyCode},
			Constraints: json.RawMessage(`{}`)}}
	claim, err := o.grants.ClaimGrant(ctx, action, proof)
	if err != nil {
		return nil, &operationError{403, "ApprovalRequired"}
	}
	if !claim.Claimed {
		return nil, &operationError{409, "MutationAlreadySubmitted"}
	}

	target := domain.ProxyReadTarget{RequestID: cmd.RequestID, EnvCode: cmd.EnvCode, ProxyCode: cmd.ProxyCode,
		GroupID: resolved.Proxy.GroupID, ServerGroupID: resolved.Proxy.ServerGroupID}
	var receipt domain.PgcatInventoryReceipt
	outcome, providerID := "not_submitted", ""
	if ctx.Err() == nil {
		receipt, err = writer.SyncPgcatInventory(ctx, resolved.Environment, target)
		if err == nil && receipt.Changed {
			outcome, providerID = "submitted", receipt.UID+":"+receipt.ResourceVersion
		}
		var commandErr *domain.CommandError
		if errors.As(err, &commandErr) && commandErr.Code == "SubmissionUnknown" {
			outcome = "unknown"
		}
	} else {
		err = ctx.Err()
	}
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if e := o.grants.RecordGrant(record, action, outcome, providerID); e != nil {
		return nil, &operationError{503, "SubmissionUnknown"}
	}
	if err != nil {
		return nil, commandFailure(err)
	}
	return receipt, nil
}
