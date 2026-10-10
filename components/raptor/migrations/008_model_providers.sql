CREATE TABLE IF NOT EXISTS raptor.model_provider_connections (
  provider_id text PRIMARY KEY,
  connection_version bigint NOT NULL DEFAULT 1 CHECK (connection_version > 0),
  status text NOT NULL CHECK (status IN ('disconnected','connected','needs_attention')),
  account_label text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS raptor.model_provider_credentials (
  provider_id text PRIMARY KEY REFERENCES raptor.model_provider_connections(provider_id),
  ciphertext bytea NOT NULL,
  nonce bytea NOT NULL,
  key_version text NOT NULL,
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  expires_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS raptor.model_provider_models (
  provider_id text NOT NULL REFERENCES raptor.model_provider_connections(provider_id),
  model_id text NOT NULL,
  display_name text NOT NULL,
  available boolean NOT NULL DEFAULT true,
  discovered_at timestamptz NOT NULL,
  PRIMARY KEY (provider_id,model_id)
);

CREATE TABLE IF NOT EXISTS raptor.model_provider_policy (
  provider_id text PRIMARY KEY REFERENCES raptor.model_provider_connections(provider_id),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  enabled_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(enabled_ids) = 'array'),
  default_model_id text NOT NULL DEFAULT '',
  updated_by uuid REFERENCES raptor.users(id),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS raptor.model_provider_login_sessions (
  session_id uuid PRIMARY KEY,
  provider_id text NOT NULL REFERENCES raptor.model_provider_connections(provider_id),
  admin_id uuid NOT NULL REFERENCES raptor.users(id),
  status text NOT NULL CHECK (status IN ('pending','connected','cancelled','expired','failed')),
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS raptor.model_provider_audit (
  id bigserial PRIMARY KEY,
  actor_id uuid NOT NULL REFERENCES raptor.users(id),
  provider_id text NOT NULL,
  action text NOT NULL,
  result text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now()
);

REVOKE ALL ON raptor.model_provider_connections,raptor.model_provider_credentials,raptor.model_provider_models,raptor.model_provider_policy,raptor.model_provider_login_sessions,raptor.model_provider_audit FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE,DELETE ON raptor.model_provider_connections,raptor.model_provider_credentials,raptor.model_provider_models,raptor.model_provider_policy,raptor.model_provider_login_sessions,raptor.model_provider_audit TO raptor_app;
GRANT USAGE,SELECT ON SEQUENCE raptor.model_provider_audit_id_seq TO raptor_app;
