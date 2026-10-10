package modelproviders

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidModel = errors.New("invalid model selection")
var ErrPolicyConflict = errors.New("model policy conflict")

type EnabledModel struct {
	ProviderID  ProviderID `json:"providerId"`
	ModelID     ModelID    `json:"modelId"`
	DisplayName string     `json:"displayName"`
	Default     bool       `json:"default"`
}

type DiscoveredModel struct {
	ModelID     ModelID `json:"modelId"`
	DisplayName string  `json:"displayName"`
}

type PolicyStore struct{ Pool *pgxpool.Pool }

func (s PolicyStore) ReplaceCatalog(ctx context.Context, provider ProviderID, models []DiscoveredModel, actorID ...string) error {
	if provider != "codex" || len(models) == 0 {
		return ErrInvalidModel
	}
	seen := map[ModelID]bool{}
	for _, m := range models {
		if m.ModelID == "" || m.DisplayName == "" || seen[m.ModelID] {
			return ErrInvalidModel
		}
		seen[m.ModelID] = true
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE raptor.model_provider_models SET available=false WHERE provider_id=$1`, provider); err != nil {
		return err
	}
	for _, m := range models {
		if _, err = tx.Exec(ctx, `INSERT INTO raptor.model_provider_models(provider_id,model_id,display_name,available,discovered_at) VALUES($1,$2,$3,true,$4)
ON CONFLICT(provider_id,model_id) DO UPDATE SET display_name=EXCLUDED.display_name,available=true,discovered_at=EXCLUDED.discovered_at`, provider, m.ModelID, m.DisplayName, time.Now()); err != nil {
			return err
		}
	}
	if len(actorID) > 0 {
		if len(actorID) != 1 || actorID[0] == "" {
			return ErrInvalidModel
		}
		if _, err = tx.Exec(ctx, `INSERT INTO raptor.model_provider_audit(actor_id,provider_id,action,result) VALUES($1,$2,'discover','success')`, actorID[0], provider); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s PolicyStore) ListEnabled(ctx context.Context) ([]EnabledModel, error) {
	rows, err := s.Pool.Query(ctx, `SELECT m.provider_id,m.model_id,m.display_name,(p.default_model_id=m.model_id)
FROM raptor.model_provider_models m JOIN raptor.model_provider_connections c USING(provider_id) JOIN raptor.model_provider_policy p USING(provider_id)
WHERE c.status='connected' AND m.available AND p.enabled_ids ? m.model_id ORDER BY m.provider_id,m.model_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EnabledModel{}
	for rows.Next() {
		var m EnabledModel
		if err = rows.Scan(&m.ProviderID, &m.ModelID, &m.DisplayName, &m.Default); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s PolicyStore) ValidateSelection(ctx context.Context, provider ProviderID, model ModelID) (int64, error) {
	if provider == "" || model == "" {
		return 0, ErrInvalidModel
	}
	var version int64
	err := s.Pool.QueryRow(ctx, `SELECT c.connection_version FROM raptor.model_provider_connections c
JOIN raptor.model_provider_models m USING(provider_id) JOIN raptor.model_provider_policy p USING(provider_id)
WHERE c.provider_id=$1 AND m.model_id=$2 AND c.status='connected' AND m.available AND p.enabled_ids ? m.model_id`, provider, model).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrInvalidModel
	}
	return version, err
}

func (s PolicyStore) SetPolicy(ctx context.Context, actor string, provider ProviderID, expectedVersion int64, enabled []ModelID, defaultID ModelID) error {
	if actor == "" || provider != "codex" || expectedVersion < 0 {
		return ErrInvalidModel
	}
	seen := map[ModelID]bool{}
	for _, id := range enabled {
		if id == "" || seen[id] {
			return ErrInvalidModel
		}
		seen[id] = true
	}
	if (len(enabled) == 0 && defaultID != "") || (len(enabled) > 0 && !seen[defaultID]) {
		return ErrInvalidModel
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM raptor.model_provider_connections WHERE provider_id=$1 FOR UPDATE`, provider).Scan(&status); err != nil {
		return ErrInvalidModel
	}
	if status != "connected" {
		return ErrInvalidModel
	}
	for _, id := range enabled {
		var ok bool
		if err = tx.QueryRow(ctx, `SELECT available FROM raptor.model_provider_models WHERE provider_id=$1 AND model_id=$2`, provider, id).Scan(&ok); err != nil || !ok {
			return ErrInvalidModel
		}
	}
	encoded, _ := json.Marshal(enabled)
	var version int64
	if expectedVersion == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO raptor.model_provider_policy(provider_id,version,enabled_ids,default_model_id,updated_by) VALUES($1,1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING version`, provider, encoded, defaultID, actor).Scan(&version)
	} else {
		err = tx.QueryRow(ctx, `UPDATE raptor.model_provider_policy SET version=version+1,enabled_ids=$3,default_model_id=$4,updated_by=$5,updated_at=now() WHERE provider_id=$1 AND version=$2 RETURNING version`, provider, expectedVersion, encoded, defaultID, actor).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPolicyConflict
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO raptor.model_provider_audit(actor_id,provider_id,action,result) VALUES($1,$2,'set_policy','success')`, actor, provider); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
