package modelproviders

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrLeaseDenied = errors.New("model credential lease denied")

type AttemptBinding struct {
	RequestID         string     `json:"requestId"`
	AttemptID         string     `json:"attemptId"`
	ProviderID        ProviderID `json:"providerId"`
	ModelID           ModelID    `json:"modelId"`
	ConnectionVersion int64      `json:"connectionVersion"`
}
type CredentialLease struct {
	Credential json.RawMessage `json:"credential"`
	Generation int64           `json:"generation"`
	ExpiresAt  time.Time       `json:"expiresAt"`
}

func (s *HTTPService) ValidateAttempt(ctx context.Context, b AttemptBinding) error {
	var version int64
	err := s.Pool.QueryRow(ctx, `SELECT c.connection_version FROM raptor.model_provider_attempt_leases l
JOIN raptor.agent_attempt_access a ON a.request_id=l.request_id AND a.attempt_id=l.attempt_id AND NOT a.revoked AND a.expires_at>now()
JOIN raptor.requests r ON r.id=l.request_id JOIN raptor.model_provider_connections c USING(provider_id) JOIN raptor.model_provider_policy p USING(provider_id)
JOIN raptor.model_provider_models m ON m.provider_id=l.provider_id AND m.model_id=l.model_id
WHERE l.attempt_id=$1 AND l.request_id=$2 AND l.provider_id=$3 AND l.model_id=$4 AND l.connection_version=$5 AND l.expires_at>now()
AND r.status NOT IN ('cancelled','failed') AND c.status='connected' AND c.connection_version=l.connection_version AND m.available AND p.enabled_ids ? m.model_id`, b.AttemptID, b.RequestID, b.ProviderID, b.ModelID, b.ConnectionVersion).Scan(&version)
	if err != nil || version != b.ConnectionVersion {
		return ErrLeaseDenied
	}
	return nil
}

func (s *HTTPService) LeaseCredential(ctx context.Context, b AttemptBinding) (CredentialLease, error) {
	if b.RequestID == "" || b.AttemptID == "" || b.ProviderID != "codex" || b.ModelID == "" || b.ConnectionVersion < 1 {
		return CredentialLease{}, ErrLeaseDenied
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return CredentialLease{}, ErrCredentialStore
	}
	defer tx.Rollback(ctx)
	var definition []byte
	var status string
	var connectionVersion, generation int64
	var encrypted Ciphertext
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT r.definition,r.status,c.connection_version,k.credential_generation,k.ciphertext,k.nonce,k.key_version,k.expires_at
FROM raptor.requests r JOIN raptor.agent_attempt_access a ON a.request_id=r.id AND a.attempt_id=$5 AND NOT a.revoked AND a.expires_at>now()
JOIN raptor.model_provider_connections c ON c.provider_id=$2 JOIN raptor.model_provider_policy p ON p.provider_id=c.provider_id
JOIN raptor.model_provider_models m ON m.provider_id=c.provider_id AND m.model_id=$3 JOIN raptor.model_provider_credentials k ON k.provider_id=c.provider_id
WHERE r.id=$1 AND r.status NOT IN ('cancelled','failed') AND c.status='connected' AND c.connection_version=$4 AND m.available AND p.enabled_ids ? m.model_id
FOR SHARE OF r,a,c,p,m,k`, b.RequestID, b.ProviderID, b.ModelID, b.ConnectionVersion, b.AttemptID).Scan(&definition, &status, &connectionVersion, &generation, &encrypted.Data, &encrypted.Nonce, &encrypted.KeyVersion, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return CredentialLease{}, ErrLeaseDenied
	}
	if err != nil {
		return CredentialLease{}, ErrCredentialStore
	}
	var selected struct {
		ProviderID        ProviderID `json:"providerId"`
		Model             string     `json:"model"`
		ConnectionVersion int64      `json:"connectionVersion"`
	}
	if json.Unmarshal(definition, &selected) != nil || selected.ProviderID != b.ProviderID || selected.Model != string(b.ModelID) || selected.ConnectionVersion != connectionVersion || status == "cancelled" {
		return CredentialLease{}, ErrLeaseDenied
	}
	if expires.Before(time.Now().Add(5 * time.Minute)) {
		return CredentialLease{}, ErrCredentialUnavailable
	}
	plain, err := DecryptCredential(encrypted, s.Credentials.Key, b.ProviderID)
	if err != nil {
		return CredentialLease{}, ErrCredentialUnavailable
	}
	tag, err := tx.Exec(ctx, `INSERT INTO raptor.model_provider_attempt_leases(attempt_id,request_id,provider_id,model_id,connection_version,credential_generation,expires_at) VALUES($1,$2,$3,$4,$5,$6,now()+interval '16 minutes') ON CONFLICT DO NOTHING`, b.AttemptID, b.RequestID, b.ProviderID, b.ModelID, b.ConnectionVersion, generation)
	if err != nil {
		return CredentialLease{}, ErrCredentialStore
	}
	if tag.RowsAffected() != 1 {
		return CredentialLease{}, ErrLeaseDenied
	}
	if tx.Commit(ctx) != nil {
		return CredentialLease{}, ErrCredentialStore
	}
	return CredentialLease{Credential: plain, Generation: generation, ExpiresAt: expires}, nil
}

func (s *HTTPService) RefreshCredential(ctx context.Context, b AttemptBinding, expectedGeneration int64, next []byte) (int64, error) {
	if expectedGeneration < 1 || len(next) == 0 {
		return 0, ErrLeaseDenied
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, ErrCredentialStore
	}
	defer tx.Rollback(ctx)
	var actualGeneration int64
	err = tx.QueryRow(ctx, `SELECT l.credential_generation FROM raptor.model_provider_attempt_leases l JOIN raptor.agent_attempt_access a ON a.request_id=l.request_id AND a.attempt_id=l.attempt_id AND NOT a.revoked AND a.expires_at>now() WHERE l.attempt_id=$1 AND l.request_id=$2 AND l.provider_id=$3 AND l.model_id=$4 AND l.connection_version=$5 AND l.expires_at>now() FOR UPDATE OF l`, b.AttemptID, b.RequestID, b.ProviderID, b.ModelID, b.ConnectionVersion).Scan(&actualGeneration)
	if err != nil || actualGeneration != expectedGeneration {
		return 0, ErrLeaseDenied
	}
	updated, err := s.Credentials.replaceCredential(ctx, tx, b.ProviderID, b.ConnectionVersion, expectedGeneration, next)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE raptor.model_provider_attempt_leases SET credential_generation=$2 WHERE attempt_id=$1`, b.AttemptID, updated.Generation)
	if err != nil {
		return 0, ErrCredentialStore
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, ErrCredentialStore
	}
	return updated.Generation, nil
}
