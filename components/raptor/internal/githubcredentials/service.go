package githubcredentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnavailable = errors.New("GitHub credential unavailable")
var ErrInvalid = errors.New("invalid GitHub credential")

type SecretStore interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
type Status struct {
	Configured bool      `json:"configured"`
	Owner      string    `json:"owner,omitempty"`
	VerifiedAt time.Time `json:"verifiedAt,omitempty"`
}
type Service struct {
	Pool    *pgxpool.Pool
	Secrets SecretStore
	Verify  func(context.Context, string) (string, error)
}

func (s *Service) Save(ctx context.Context, actor, token string) (Status, error) {
	if actor == "" || strings.TrimSpace(token) != token || len(token) < 16 || len(token) > 1024 || strings.ContainsAny(token, "\r\n\x00") || s.Pool == nil || s.Secrets == nil || s.Verify == nil {
		return Status{}, ErrInvalid
	}
	owner, err := s.Verify(ctx, token)
	if err != nil || owner == "" {
		return Status{}, ErrInvalid
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Status{}, ErrUnavailable
	}
	ref := "github-pat/" + hex.EncodeToString(nonce[:])
	if err = s.Secrets.Put(ctx, ref, []byte(token)); err != nil {
		return Status{}, ErrUnavailable
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		_ = s.Secrets.Delete(ctx, ref)
		return Status{}, ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var old string
	err = tx.QueryRow(ctx, "SELECT secret_ref FROM raptor.github_pat_setting WHERE id=1 FOR UPDATE").Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		_ = s.Secrets.Delete(ctx, ref)
		return Status{}, ErrUnavailable
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO raptor.github_pat_setting(id,secret_ref,owner_label,verified_at,updated_by) VALUES(1,$1,$2,$3,$4)
 ON CONFLICT(id) DO UPDATE SET secret_ref=EXCLUDED.secret_ref,owner_label=EXCLUDED.owner_label,verified_at=EXCLUDED.verified_at,updated_by=EXCLUDED.updated_by,updated_at=now()`, ref, owner, now, actor)
	if err == nil && old != "" && old != ref {
		_, err = tx.Exec(ctx, "INSERT INTO raptor.github_pat_cleanup(secret_ref) VALUES($1) ON CONFLICT DO NOTHING", old)
	}
	if err != nil || tx.Commit(ctx) != nil {
		_ = s.Secrets.Delete(ctx, ref)
		return Status{}, ErrUnavailable
	}
	if old != "" && old != ref {
		if s.Cleanup(ctx) != nil {
			return Status{Configured: true, Owner: owner, VerifiedAt: now}, ErrUnavailable
		}
	}
	return Status{Configured: true, Owner: owner, VerifiedAt: now}, nil
}
func (s *Service) Cleanup(ctx context.Context) error {
	rows, err := s.Pool.Query(ctx, "SELECT secret_ref FROM raptor.github_pat_cleanup ORDER BY queued_at")
	if err != nil {
		return ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return ErrUnavailable
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrUnavailable
	}
	for _, ref := range refs {
		if s.Secrets.Delete(ctx, ref) != nil {
			return ErrUnavailable
		}
		if _, err = s.Pool.Exec(ctx, "DELETE FROM raptor.github_pat_cleanup WHERE secret_ref=$1", ref); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	var out Status
	err := s.Pool.QueryRow(ctx, "SELECT owner_label,verified_at FROM raptor.github_pat_setting WHERE id=1").Scan(&out.Owner, &out.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, ErrUnavailable
	}
	out.Configured = true
	return out, nil
}
func (s *Service) Load(ctx context.Context) ([]byte, error) {
	var ref string
	if s.Pool.QueryRow(ctx, "SELECT secret_ref FROM raptor.github_pat_setting WHERE id=1").Scan(&ref) != nil {
		return nil, ErrUnavailable
	}
	token, err := s.Secrets.Get(ctx, ref)
	if err != nil || len(token) == 0 {
		return nil, ErrUnavailable
	}
	return token, nil
}
func (s *Service) Delete(ctx context.Context, actor string) error {
	if actor == "" {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var ref string
	err = tx.QueryRow(ctx, "SELECT secret_ref FROM raptor.github_pat_setting WHERE id=1 FOR UPDATE").Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || s.Secrets.Delete(ctx, ref) != nil {
		return ErrUnavailable
	}
	if _, err = tx.Exec(ctx, "DELETE FROM raptor.github_pat_setting WHERE id=1"); err != nil || tx.Commit(ctx) != nil {
		return ErrUnavailable
	}
	return nil
}
