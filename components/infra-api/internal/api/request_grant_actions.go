package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/commands"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"raptor-iap/infra-api/internal/resources"
)

const requestGrantHeader = "X-Raptor-Request-Grant"

var grantIDPattern = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)

type RequestGrantClient interface {
	ClaimGrant(context.Context, approval.GrantAction, resources.Proof) (approval.GrantReceipt, error)
	DispatchGrant(context.Context, approval.GrantAction) error
	RecordGrant(context.Context, approval.GrantAction, string, string) error
}

func (o resourceOperations) grantDispatch(action approval.GrantAction) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := o.grants.DispatchGrant(ctx, action); err != nil {
			return &domain.CommandError{Code: "ApprovalRequired"}
		}
		return nil
	}
}

type pgcatStateReader interface {
	ReadPgcatRequestState(context.Context, string, string) (approval.PgcatRequestState, error)
}

type pgcatGrantPlanner interface {
	PreparePgcatGrant(context.Context, domain.Environment, commands.PgcatGrantInput) (commands.PreparedPgcatGrant, error)
}

func (o resourceOperations) grantedPgcatCommand(ctx context.Context, identity AgentIdentity, resolved domain.ResolvedResource, tool string, args any, raw json.RawMessage) (any, error) {
	planner, plannerOK := o.commander.(pgcatGrantPlanner)
	reader, readerOK := o.grants.(pgcatStateReader)
	if !plannerOK || !readerOK {
		return nil, &operationError{503, "RequestGrantEnforcementUnavailable"}
	}
	proof, err := resourceIdentityProof(identity)
	if err != nil {
		return nil, err
	}
	binding, scope := identity.Binding, identity.ReplacementScope
	target := domain.ProxyTarget{RequestID: binding.RequestID, EnvCode: binding.EnvCode, ProxyCode: scope.ProxyCode, GroupID: scope.GroupID}
	actionID := ""
	kind, code := "db-proxy", scope.ProxyCode
	var command any
	switch in := args.(type) {
	case resourceScale:
		actionID = in.ActionID
		command = domain.ScaleCommand{ProxyTarget: target, ActionID: actionID, DesiredCapacity: in.DesiredCapacity}
	case resourceProtection:
		command = domain.ProtectionCommand{ProxyTarget: target, InstanceIDs: in.NodeIDs, Protected: in.Protected}
	case resourceDeregister:
		command = domain.DeregisterCommand{ProxyTarget: target, ServerGroupID: scope.ServerGroupID, InstanceIDs: in.NodeIDs}
	case resourceRestart:
		app, err := selectApplication(resolved, in.WorkloadKey, &identity)
		if err != nil {
			return nil, err
		}
		t := app.Target
		command = domain.RestartCommand{RequestID: binding.RequestID, EnvCode: t.EnvCode, AppCode: t.AppCode, ClusterID: t.ClusterID,
			Namespace: t.Namespace, Name: t.Name, UID: t.UID}
		kind, code = "application", t.AppCode
		resolved, err = o.resolver.Resolve(ctx, binding.EnvCode, "db-proxy", scope.ProxyCode)
		if err != nil {
			return nil, commandFailure(err)
		}
		if err := compareResolvedProof(identity, resolved); err != nil {
			return nil, err
		}
	default:
		return nil, &operationError{400, "InvalidInput"}
	}
	if actionID == "" {
		canonical, _ := json.Marshal(command)
		hash := sha256.Sum256(append([]byte(tool+"\x00"), canonical...))
		actionID = hex.EncodeToString(hash[:])
	}
	state, err := reader.ReadPgcatRequestState(ctx, binding.RequestID, scope.ResolutionDigest)
	if err != nil {
		return nil, &operationError{503, "EvidenceUnavailable"}
	}
	prepared, err := planner.PreparePgcatGrant(ctx, resolved.Environment, commands.PgcatGrantInput{Target: target, ScopeDigest: scope.ResolutionDigest,
		ActionID: actionID, Operation: command, State: state})
	if err != nil {
		return nil, commandFailure(err)
	}
	rules, ok := policy.ApprovalPolicyForTool(tool, binding.ApprovalProtocol)
	if !ok || !slices.ContainsFunc(rules.Variants, func(rule policy.ApprovalVariant) bool {
		return rule.Capability == prepared.Capability && rule.TargetKind == kind && rule.RequiresHumanApproval
	}) || prepared.Submit == nil {
		return nil, &operationError{403, "ApprovalRequired"}
	}
	action := approval.GrantAction{RequestID: binding.RequestID, GrantID: o.grantID, ActionID: actionID, EnvCode: binding.EnvCode,
		ScopeDigest: scope.ResolutionDigest, Arguments: raw, Capability: approval.GrantCapability{
			Capability: prepared.Capability, Target: approval.GrantTarget{Kind: kind, Code: code}, Constraints: prepared.Constraints}}
	receipt, err := o.grants.ClaimGrant(ctx, action, proof)
	if err != nil {
		return nil, &operationError{403, "ApprovalRequired"}
	}
	if !receipt.Claimed {
		return nil, &operationError{409, "MutationAlreadySubmitted"}
	}
	data, providerID, err := prepared.Submit(ctx, o.grantDispatch(action))
	outcome := "not_submitted"
	var providerErr *domain.CommandError
	switch {
	case err == nil && providerID != "":
		outcome = "submitted"
	case err == nil:
		outcome = "unknown"
		err = &domain.CommandError{Code: "SubmissionUnknown"}
	case errors.As(err, &providerErr) && providerErr.Code == "SubmissionUnknown":
		outcome = "unknown"
	}
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := o.grants.RecordGrant(record, action, outcome, providerID); err != nil {
		return nil, &operationError{503, "SubmissionUnknown"}
	}
	if err != nil {
		return nil, commandFailure(err)
	}
	raw, _ = json.Marshal(data)
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, &operationError{503, "ProviderUnavailable"}
	}
	result["actionId"], _ = json.Marshal(actionID)
	return result, nil
}

type pgcatRetirementReader interface {
	ObservePgcatRetirement(context.Context, domain.Environment, domain.ProxyReadTarget, string) (domain.PgcatRetirementObservation, error)
}

func pgcatOriginalSockets(state approval.PgcatRequestState) map[string]json.RawMessage {
	for _, observation := range state.Observations {
		if observation.Kind == "hold" && observation.Subject == "fleet" {
			var hold struct {
				Sockets map[string]json.RawMessage `json:"sockets"`
			}
			if json.Unmarshal(observation.Data, &hold) == nil {
				return hold.Sockets
			}
			break
		}
	}
	return nil
}

func (o resourceOperations) pgcatExpansionRead(ctx context.Context, identity *AgentIdentity, resolved domain.ResolvedResource) (any, error) {
	if identity == nil || identity.Binding == nil || identity.Binding.ApprovalProtocol != policy.RequestGrantProtocol ||
		identity.Binding.ExecutionMode != "replacement" {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	reader, ok := o.grants.(pgcatStateReader)
	provider, providerOK := o.reader.(commands.PgcatExpansionReader)
	if !ok || !providerOK {
		return nil, &operationError{503, "EvidenceUnavailable"}
	}
	b, s := identity.Binding, identity.ReplacementScope
	state, err := reader.ReadPgcatRequestState(ctx, b.RequestID, s.ResolutionDigest)
	if err != nil {
		return nil, &operationError{503, "EvidenceUnavailable"}
	}
	observed, err := commands.ObserveGrantedExpansion(ctx, provider, resolved.Environment,
		domain.ProxyTarget{RequestID: b.RequestID, EnvCode: b.EnvCode, ProxyCode: s.ProxyCode, GroupID: s.GroupID}, s.ResolutionDigest, state)
	if err != nil {
		return nil, commandFailure(err)
	}
	w := resolved.PgproxyWorkload
	return map[string]any{"envCode": s.EnvCode, "proxyCode": s.ProxyCode, "observedAt": observed.ObservedAt,
		"pgproxyWorkload": map[string]any{"type": "ess", "code": w.Code, "workloadKey": w.WorkloadKey, "state": w.State,
			"targetDbCode": w.TargetDBCode, "expansion": observed}}, nil
}

func (o resourceOperations) pgcatRetirementRead(ctx context.Context, identity *AgentIdentity, resolved domain.ResolvedResource) (any, error) {
	if identity == nil || identity.Binding == nil || identity.Binding.ApprovalProtocol != policy.RequestGrantProtocol ||
		identity.Binding.ExecutionMode != "replacement" {
		return nil, &operationError{403, "ScopeMismatch"}
	}
	reader, ok := o.grants.(pgcatStateReader)
	provider, providerOK := o.reader.(pgcatRetirementReader)
	if !ok || !providerOK {
		return nil, &operationError{503, "EvidenceUnavailable"}
	}
	b, s := identity.Binding, identity.ReplacementScope
	state, err := reader.ReadPgcatRequestState(ctx, b.RequestID, s.ResolutionDigest)
	if err != nil {
		return nil, &operationError{503, "EvidenceUnavailable"}
	}
	var action, activity string
	for _, item := range state.Actions {
		if item.Capability.Capability != "db-proxy.scale-in" || item.State != "submitted" {
			continue
		}
		providerID, activityID, valid := strings.Cut(item.ProviderRequestID, ":")
		if !valid || providerID == "" || !strings.HasPrefix(activityID, "asa-") || action != "" || item.RequestID != b.RequestID ||
			item.ScopeDigest != s.ResolutionDigest || item.Capability.Target.Code != s.ProxyCode {
			return nil, &operationError{409, "IdentityChanged"}
		}
		action, activity = item.ActionID, activityID
	}
	originals := pgcatOriginalSockets(state)
	if action == "" || len(originals) != 2 {
		return nil, &operationError{409, "MissingEvidence"}
	}
	target := domain.ProxyReadTarget{RequestID: b.RequestID, EnvCode: s.EnvCode, ProxyCode: s.ProxyCode, GroupID: s.GroupID, ServerGroupID: s.ServerGroupID}
	observed, err := provider.ObservePgcatRetirement(ctx, resolved.Environment, target, activity)
	if err != nil {
		return nil, commandFailure(err)
	}
	if observed.Status == "Successful" && (len(observed.DestroyedIDs) != 2 ||
		originals[observed.DestroyedIDs[0]] == nil || originals[observed.DestroyedIDs[1]] == nil) {
		return nil, &operationError{409, "IdentityChanged"}
	}
	observed.ActionID = action
	w := resolved.PgproxyWorkload
	return map[string]any{"envCode": s.EnvCode, "proxyCode": s.ProxyCode, "observedAt": observed.ObservedAt,
		"pgproxyWorkload": map[string]any{"type": "ess", "code": w.Code, "workloadKey": w.WorkloadKey, "state": w.State,
			"targetDbCode": w.TargetDBCode, "retirement": observed}}, nil
}
