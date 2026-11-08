package approvals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type GrantTarget struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}

type GrantCapability struct {
	Capability  string          `json:"capability"`
	Target      GrantTarget     `json:"target"`
	Constraints json.RawMessage `json:"constraints"`
}

type RequestGrantInput struct {
	RequestID    string            `json:"requestId"`
	EnvCode      string            `json:"envCode"`
	ScopeDigest  string            `json:"scopeDigest"`
	Capabilities []GrantCapability `json:"capabilities"`
}

type RequestGrant struct {
	RequestGrantInput
	ID           string     `json:"id"`
	BundleDigest string     `json:"bundleDigest"`
	State        string     `json:"state"`
	Version      int        `json:"version"`
	DecidedBy    string     `json:"decidedBy,omitempty"`
	DecidedAt    *time.Time `json:"decidedAt,omitempty"`
}

type GrantActionInput struct {
	RequestID   string          `json:"requestId"`
	GrantID     string          `json:"grantId"`
	ActionID    string          `json:"actionId"`
	EnvCode     string          `json:"envCode"`
	ScopeDigest string          `json:"scopeDigest"`
	Capability  GrantCapability `json:"capability"`
	Arguments   json.RawMessage `json:"arguments"`
}

type GrantActionReceipt struct {
	Claimed           bool   `json:"claimed"`
	State             string `json:"state"`
	ProviderRequestID string `json:"providerRequestId,omitempty"`
}

type GrantActionRecord struct {
	Action            GrantActionInput `json:"action"`
	Outcome           string           `json:"outcome"`
	ProviderRequestID string           `json:"providerRequestId,omitempty"`
}

const (
	grantVersion = 1
	grantColumns = `id::text, request_id::text, env_code, scope_digest, bundle_digest, capabilities, state, version,
		coalesce(decided_by::text, ''), decided_at`
)

func grantIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value)
}

func grantUUID(value string) bool {
	var id pgtype.UUID
	return value != "" && id.Scan(value) == nil && id.Valid
}

func grantScopeValid(requestID, envCode, digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return grantUUID(requestID) && grantIdentifier(envCode) && err == nil && len(decoded) == sha256.Size &&
		digest == strings.ToLower(digest)
}

func canonicalGrantObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 8192 || !json.Valid(raw) {
		return nil, domain.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var object map[string]any
	if err := d.Decode(&object); err != nil || object == nil {
		return nil, domain.ErrInvalid
	}
	return json.Marshal(object)
}

func normalizeGrantCapability(c GrantCapability) (GrantCapability, error) {
	if !grantIdentifier(c.Capability) || !grantIdentifier(c.Target.Kind) || !grantIdentifier(c.Target.Code) {
		return GrantCapability{}, domain.ErrInvalid
	}
	constraints, err := canonicalGrantObject(c.Constraints)
	if err != nil {
		return GrantCapability{}, err
	}
	c.Constraints = constraints
	return c, nil
}

func digestGrantValue(value any) string {
	// All callers pass the versioned, normalized wire types above.
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func normalizeGrant(in RequestGrantInput) (RequestGrantInput, error) {
	if !grantScopeValid(in.RequestID, in.EnvCode, in.ScopeDigest) || len(in.Capabilities) == 0 || len(in.Capabilities) > 16 {
		return RequestGrantInput{}, domain.ErrInvalid
	}
	items := make([]GrantCapability, len(in.Capabilities))
	for i, capability := range in.Capabilities {
		normalized, err := normalizeGrantCapability(capability)
		if err != nil {
			return RequestGrantInput{}, err
		}
		items[i] = normalized
	}
	compare := func(a, b GrantCapability) int {
		if c := strings.Compare(a.Capability, b.Capability); c != 0 {
			return c
		}
		if c := strings.Compare(a.Target.Kind, b.Target.Kind); c != 0 {
			return c
		}
		if c := strings.Compare(a.Target.Code, b.Target.Code); c != 0 {
			return c
		}
		return bytes.Compare(a.Constraints, b.Constraints)
	}
	slices.SortFunc(items, compare)
	for i := 1; i < len(items); i++ {
		if compare(items[i-1], items[i]) == 0 {
			return RequestGrantInput{}, domain.ErrInvalid
		}
	}
	in.Capabilities = items
	return in, nil
}

func normalizeGrantAction(in GrantActionInput) (GrantActionInput, error) {
	if !grantScopeValid(in.RequestID, in.EnvCode, in.ScopeDigest) || !grantUUID(in.GrantID) || !grantIdentifier(in.ActionID) {
		return GrantActionInput{}, domain.ErrInvalid
	}
	capability, err := normalizeGrantCapability(in.Capability)
	if err != nil {
		return GrantActionInput{}, err
	}
	arguments, err := canonicalGrantObject(in.Arguments)
	if err != nil {
		return GrantActionInput{}, err
	}
	in.Capability, in.Arguments = capability, arguments
	return in, nil
}

func scanRequestGrant(row pgx.Row) (RequestGrant, error) {
	var out RequestGrant
	err := row.Scan(&out.ID, &out.RequestID, &out.EnvCode, &out.ScopeDigest, &out.BundleDigest, &out.Capabilities,
		&out.State, &out.Version, &out.DecidedBy, &out.DecidedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return RequestGrant{}, domain.ErrNotFound
	case err != nil:
		return RequestGrant{}, domain.ErrUnavailable
	}
	return out, nil
}

type grantRequestState struct {
	status, kind, env, control, creator string
}

func lockGrantRequest(ctx context.Context, tx pgx.Tx, requestID string) (grantRequestState, error) {
	var state grantRequestState
	err := tx.QueryRow(ctx, `SELECT status, coalesce(definition->>'type',''), coalesce(definition->>'envCode',''),
		control_state, creator_id::text FROM raptor.requests WHERE id=$1 FOR UPDATE`, requestID).
		Scan(&state.status, &state.kind, &state.env, &state.control, &state.creator)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return state, domain.ErrNotFound
	case err != nil:
		return state, domain.ErrUnavailable
	}
	return state, nil
}

func (s *Service) verifyRequestGrant(ctx context.Context, in RequestGrantInput) (ResourceVerification, error) {
	if s.VerifyGrantScope == nil {
		return nil, domain.ErrUnavailable
	}
	verify, err := s.VerifyGrantScope(ctx, in)
	if err == nil && verify == nil {
		return nil, domain.ErrUnavailable
	}
	return verify, err
}

func (s *Service) RequestGrant(ctx context.Context, input RequestGrantInput) (RequestGrant, error) {
	in, err := normalizeGrant(input)
	if err != nil {
		return RequestGrant{}, err
	}
	verify, err := s.verifyRequestGrant(ctx, in)
	if err != nil {
		return RequestGrant{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return RequestGrant{}, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	state, err := lockGrantRequest(ctx, tx, in.RequestID)
	if err != nil {
		return RequestGrant{}, err
	}
	if state.kind != "agent" || state.env != in.EnvCode {
		return RequestGrant{}, domain.ErrForbidden
	}
	if state.control != "" || (state.status != "running" && state.status != "waiting_approval") {
		return RequestGrant{}, domain.ErrConflict
	}
	if err := verify(ctx, tx); err != nil {
		return RequestGrant{}, err
	}

	digest := digestGrantValue(struct {
		Version int
		RequestGrantInput
	}{grantVersion, in})
	_, err = tx.Exec(ctx, `INSERT INTO raptor.request_grants(id, request_id, env_code, scope_digest, bundle_digest, capabilities, version)
		VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		domain.NewID(), in.RequestID, in.EnvCode, in.ScopeDigest, digest, in.Capabilities, grantVersion)
	if err != nil {
		return RequestGrant{}, domain.ErrUnavailable
	}
	out, err := scanRequestGrant(tx.QueryRow(ctx, "SELECT "+grantColumns+`
		FROM raptor.request_grants WHERE request_id=$1 AND bundle_digest=$2`, in.RequestID, digest))
	if errors.Is(err, domain.ErrNotFound) {
		return RequestGrant{}, domain.ErrConflict
	}
	if err != nil {
		return RequestGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RequestGrant{}, domain.ErrUnavailable
	}
	return out, nil
}

func (s *Service) GetRequestGrant(ctx context.Context, requestID, grantID string) (RequestGrant, error) {
	if !grantUUID(requestID) || !grantUUID(grantID) {
		return RequestGrant{}, domain.ErrInvalid
	}
	return scanRequestGrant(s.Pool.QueryRow(ctx, "SELECT "+grantColumns+`
		FROM raptor.request_grants WHERE request_id=$1 AND id=$2`, requestID, grantID))
}

func (s *Service) DecideRequestGrant(ctx context.Context, user domain.User, requestID, grantID, decision string) (RequestGrant, error) {
	if !grantUUID(user.ID) || (user.Role != "admin" && user.Role != "user") {
		return RequestGrant{}, domain.ErrForbidden
	}
	next, valid := map[string]string{"approve": "approved", "deny": "denied"}[decision]
	if !valid || !grantUUID(requestID) || !grantUUID(grantID) {
		return RequestGrant{}, domain.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return RequestGrant{}, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	state, err := lockGrantRequest(ctx, tx, requestID)
	if err != nil {
		return RequestGrant{}, err
	}
	if state.kind != "agent" || (user.Role != "admin" && state.creator != user.ID) {
		return RequestGrant{}, domain.ErrForbidden
	}
	out, err := scanRequestGrant(tx.QueryRow(ctx, "SELECT "+grantColumns+`
		FROM raptor.request_grants WHERE request_id=$1 AND id=$2`, requestID, grantID))
	if err != nil {
		return RequestGrant{}, err
	}
	if out.State == next {
		return out, nil
	}
	if out.State != "pending" || state.env != out.EnvCode || state.control != "" ||
		(state.status != "running" && state.status != "waiting_approval") {
		return RequestGrant{}, domain.ErrConflict
	}
	out, err = scanRequestGrant(tx.QueryRow(ctx, `UPDATE raptor.request_grants
		SET state=$3, decided_by=$4, decided_at=clock_timestamp()
		WHERE request_id=$1 AND id=$2 RETURNING `+grantColumns, requestID, grantID, next, user.ID))
	if err != nil {
		return RequestGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RequestGrant{}, domain.ErrUnavailable
	}
	return out, nil
}

type grantReceiptQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readGrantReceipt(ctx context.Context, q grantReceiptQuery, in GrantActionInput, digest string, lock bool) (GrantActionReceipt, error) {
	query := `SELECT grant_id::text, input_digest, state, provider_request_id FROM raptor.request_grant_actions
		WHERE request_id=$1 AND action_id=$2`
	if lock {
		query += " FOR UPDATE"
	}
	var grantID, storedDigest string
	var out GrantActionReceipt
	err := q.QueryRow(ctx, query, in.RequestID, in.ActionID).Scan(&grantID, &storedDigest, &out.State, &out.ProviderRequestID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return GrantActionReceipt{}, domain.ErrNotFound
	case err != nil:
		return GrantActionReceipt{}, domain.ErrUnavailable
	case grantID != in.GrantID || storedDigest != digest:
		return GrantActionReceipt{}, domain.ErrConflict
	}
	return out, nil
}

// ClaimGrantAction is called by the trusted Infra API after it derives the
// capability projection from the exact command and its resolved target. Only a
// new claim permits submission. An existing receipt is safe to read after close.
func (s *Service) ClaimGrantAction(ctx context.Context, input GrantActionInput) (GrantActionReceipt, error) {
	in, err := normalizeGrantAction(input)
	if err != nil {
		return GrantActionReceipt{}, err
	}
	digest := digestGrantValue(in)
	if receipt, err := readGrantReceipt(ctx, s.Pool, in, digest, false); !errors.Is(err, domain.ErrNotFound) {
		return receipt, err
	}
	verify, err := s.verifyRequestGrant(ctx, RequestGrantInput{
		RequestID: in.RequestID, EnvCode: in.EnvCode, ScopeDigest: in.ScopeDigest, Capabilities: []GrantCapability{in.Capability},
	})
	if err != nil {
		return GrantActionReceipt{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return GrantActionReceipt{}, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	state, err := lockGrantRequest(ctx, tx, in.RequestID)
	if err != nil {
		return GrantActionReceipt{}, err
	}
	if receipt, err := readGrantReceipt(ctx, tx, in, digest, false); !errors.Is(err, domain.ErrNotFound) {
		return receipt, err
	}
	if state.kind != "agent" || state.env != in.EnvCode {
		return GrantActionReceipt{}, domain.ErrForbidden
	}
	if state.control != "" || state.status != "running" {
		return GrantActionReceipt{}, domain.ErrConflict
	}

	grant, err := scanRequestGrant(tx.QueryRow(ctx, "SELECT "+grantColumns+`
		FROM raptor.request_grants WHERE request_id=$1 AND id=$2`, in.RequestID, in.GrantID))
	if errors.Is(err, domain.ErrNotFound) {
		return GrantActionReceipt{}, domain.ErrForbidden
	}
	if err != nil {
		return GrantActionReceipt{}, err
	}
	if grant.State != "approved" || grant.Version != grantVersion || grant.EnvCode != in.EnvCode || grant.ScopeDigest != in.ScopeDigest ||
		!slices.ContainsFunc(grant.Capabilities, func(allowed GrantCapability) bool {
			return allowed.Capability == in.Capability.Capability && allowed.Target == in.Capability.Target &&
				equalJSON(allowed.Constraints, in.Capability.Constraints)
		}) {
		return GrantActionReceipt{}, domain.ErrForbidden
	}
	if err := verify(ctx, tx); err != nil {
		return GrantActionReceipt{}, err
	}
	var unresolved bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM raptor.request_grant_actions
		WHERE request_id=$1 AND state IN ('claimed','unknown'))`, in.RequestID).Scan(&unresolved); err != nil {
		return GrantActionReceipt{}, domain.ErrUnavailable
	}
	if unresolved {
		return GrantActionReceipt{}, domain.ErrConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO raptor.request_grant_actions(request_id, grant_id, action_id, input_digest)
		VALUES($1,$2,$3,$4)`, in.RequestID, in.GrantID, in.ActionID, digest); err != nil {
		return GrantActionReceipt{}, domain.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return GrantActionReceipt{}, domain.ErrUnavailable
	}
	return GrantActionReceipt{Claimed: true, State: "claimed"}, nil
}

// RecordGrantAction preserves a receipt even if the request was closed after
// admission. Unknown outcomes remain blocked for reconciliation.
func (s *Service) RecordGrantAction(ctx context.Context, record GrantActionRecord) error {
	in, err := normalizeGrantAction(record.Action)
	if err != nil {
		return err
	}
	switch record.Outcome {
	case "submitted":
		if record.ProviderRequestID == "" {
			return domain.ErrInvalid
		}
	case "not_submitted":
		if record.ProviderRequestID != "" {
			return domain.ErrInvalid
		}
	case "unknown":
	default:
		return domain.ErrInvalid
	}
	if len(record.ProviderRequestID) > 512 {
		return domain.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	receipt, err := readGrantReceipt(ctx, tx, in, digestGrantValue(in), true)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrConflict
	}
	if err != nil {
		return err
	}
	if receipt.State != "claimed" {
		if receipt.State == record.Outcome && receipt.ProviderRequestID == record.ProviderRequestID {
			return nil
		}
		return domain.ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE raptor.request_grant_actions SET state=$3, provider_request_id=$4, recorded_at=clock_timestamp()
		WHERE request_id=$1 AND action_id=$2`, in.RequestID, in.ActionID, record.Outcome, record.ProviderRequestID); err != nil {
		return domain.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ErrUnavailable
	}
	return nil
}
