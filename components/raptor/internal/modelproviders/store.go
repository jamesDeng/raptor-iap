package modelproviders

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrStaleGeneration = errors.New("stale credential generation")
var ErrCredentialUnavailable = errors.New("credential unavailable")
var ErrCredentialStore = errors.New("credential store unavailable")

type CredentialRecord struct {
	ProviderID        ProviderID
	ConnectionVersion int64
	Generation        int64
	Credential        []byte
	ExpiresAt         time.Time
}
type CredentialStore struct {
	Pool       *pgxpool.Pool
	Key        []byte
	KeyVersion string
}

func NewCredentialStore(pool *pgxpool.Pool, key []byte, keyVersion string) *CredentialStore {
	return &CredentialStore{Pool: pool, Key: append([]byte(nil), key...), KeyVersion: keyVersion}
}

func credentialExpiry(raw []byte) (time.Time, error) {
	var value struct {
		Type, Access, Refresh string
		Expires               int64
	}
	if json.Unmarshal(raw, &value) != nil || value.Type != "oauth" || value.Access == "" || value.Refresh == "" || value.Expires <= time.Now().UnixMilli() {
		return time.Time{}, errors.New("invalid OAuth credential")
	}
	return time.UnixMilli(value.Expires), nil
}

func (s *CredentialStore) PutConnectedCredential(ctx context.Context, providerID ProviderID, label string, raw []byte) (CredentialRecord, error) {
	expiry, e := credentialExpiry(raw)
	if e != nil {
		return CredentialRecord{}, e
	}
	cipher, e := EncryptCredential(raw, s.Key, s.KeyVersion, providerID)
	if e != nil {
		return CredentialRecord{}, e
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return CredentialRecord{}, ErrCredentialStore
	}
	defer tx.Rollback(ctx)
	var version int64
	e = tx.QueryRow(ctx, `INSERT INTO raptor.model_provider_connections(provider_id,status,account_label) VALUES($1,'connected',$2)
 ON CONFLICT(provider_id) DO UPDATE SET connection_version=raptor.model_provider_connections.connection_version+1,status='connected',account_label=EXCLUDED.account_label,updated_at=now()
 RETURNING connection_version`, providerID, label).Scan(&version)
	if e != nil {
		return CredentialRecord{}, ErrCredentialStore
	}
	_, e = tx.Exec(ctx, `INSERT INTO raptor.model_provider_credentials(provider_id,ciphertext,nonce,key_version,credential_generation,expires_at) VALUES($1,$2,$3,$4,1,$5)
 ON CONFLICT(provider_id) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,nonce=EXCLUDED.nonce,key_version=EXCLUDED.key_version,credential_generation=1,expires_at=EXCLUDED.expires_at,updated_at=now()`, providerID, cipher.Data, cipher.Nonce, cipher.KeyVersion, expiry)
	if e != nil {
		return CredentialRecord{}, ErrCredentialStore
	}
	if tx.Commit(ctx) != nil {
		return CredentialRecord{}, ErrCredentialStore
	}
	return CredentialRecord{ProviderID: providerID, ConnectionVersion: version, Generation: 1, Credential: append([]byte(nil), raw...), ExpiresAt: expiry}, nil
}

func (s *CredentialStore) LoadCredential(ctx context.Context, providerID ProviderID) (CredentialRecord, error) {
	var record CredentialRecord
	var encrypted Ciphertext
	var status string
	e := s.Pool.QueryRow(ctx, `SELECT p.connection_version,p.status,c.credential_generation,c.ciphertext,c.nonce,c.key_version,c.expires_at
 FROM raptor.model_provider_connections p JOIN raptor.model_provider_credentials c USING(provider_id) WHERE p.provider_id=$1`, providerID).
		Scan(&record.ConnectionVersion, &status, &record.Generation, &encrypted.Data, &encrypted.Nonce, &encrypted.KeyVersion, &record.ExpiresAt)
	if e == pgx.ErrNoRows || status != "connected" {
		return CredentialRecord{}, ErrCredentialUnavailable
	}
	if e != nil {
		return CredentialRecord{}, ErrCredentialStore
	}
	raw, e := DecryptCredential(encrypted, s.Key, providerID)
	if e != nil {
		return CredentialRecord{}, ErrCredentialUnavailable
	}
	record.ProviderID = providerID
	record.Credential = raw
	return record, nil
}

func (s *CredentialStore) ReplaceCredential(ctx context.Context, providerID ProviderID, expectedVersion, expectedGeneration int64, next []byte) (CredentialRecord, error) {
	expiry, e := credentialExpiry(next)
	if e != nil {
		return CredentialRecord{}, e
	}
	cipher, e := EncryptCredential(next, s.Key, s.KeyVersion, providerID)
	if e != nil {
		return CredentialRecord{}, e
	}
	var generation int64
	e = s.Pool.QueryRow(ctx, `UPDATE raptor.model_provider_credentials c SET ciphertext=$4,nonce=$5,key_version=$6,expires_at=$7,credential_generation=c.credential_generation+1,updated_at=now()
 FROM raptor.model_provider_connections p WHERE c.provider_id=p.provider_id AND c.provider_id=$1 AND p.connection_version=$2 AND p.status='connected' AND c.credential_generation=$3
 RETURNING c.credential_generation`, providerID, expectedVersion, expectedGeneration, cipher.Data, cipher.Nonce, cipher.KeyVersion, expiry).Scan(&generation)
	if e == pgx.ErrNoRows {
		return CredentialRecord{}, ErrStaleGeneration
	}
	if e != nil {
		return CredentialRecord{}, ErrCredentialStore
	}
	return CredentialRecord{ProviderID: providerID, ConnectionVersion: expectedVersion, Generation: generation, Credential: append([]byte(nil), next...), ExpiresAt: expiry}, nil
}
