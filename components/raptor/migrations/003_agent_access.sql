CREATE TABLE IF NOT EXISTS raptor.agent_attempt_access(
 request_id uuid NOT NULL REFERENCES raptor.requests(id), attempt_id uuid NOT NULL UNIQUE,
 binding jsonb NOT NULL, expires_at timestamptz NOT NULL, revoked boolean NOT NULL DEFAULT false,
 PRIMARY KEY(request_id,attempt_id));
CREATE TABLE IF NOT EXISTS raptor.agent_tokens(
 token_hash text PRIMARY KEY, request_id uuid NOT NULL, attempt_id uuid NOT NULL,
 expires_at timestamptz NOT NULL, revoked boolean NOT NULL DEFAULT false,
 FOREIGN KEY(request_id,attempt_id) REFERENCES raptor.agent_attempt_access(request_id,attempt_id));
GRANT SELECT,INSERT,UPDATE,DELETE ON raptor.agent_attempt_access,raptor.agent_tokens TO raptor_app;
