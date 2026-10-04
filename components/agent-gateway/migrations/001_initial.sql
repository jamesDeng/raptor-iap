CREATE SCHEMA IF NOT EXISTS gateway AUTHORIZATION gateway_owner;
REVOKE ALL ON SCHEMA gateway FROM PUBLIC;
CREATE TABLE IF NOT EXISTS gateway.executions(request_id uuid PRIMARY KEY, status text NOT NULL DEFAULT 'queued', attempt_id uuid, input jsonb NOT NULL DEFAULT '{}', applied_skills jsonb NOT NULL DEFAULT '{}', checkpoint jsonb NOT NULL DEFAULT '{}', cleanup text NOT NULL DEFAULT 'confirmed', recovery_needed boolean NOT NULL DEFAULT false, next_sequence bigint NOT NULL DEFAULT 0, queued_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS gateway.attempts(id uuid PRIMARY KEY, request_id uuid NOT NULL REFERENCES gateway.executions(request_id), state text NOT NULL, runtime_id text NOT NULL DEFAULT '', started_at timestamptz NOT NULL DEFAULT now(), ended_at timestamptz);
CREATE TABLE IF NOT EXISTS gateway.events(request_id uuid NOT NULL REFERENCES gateway.executions(request_id), sequence bigint NOT NULL, event_id text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(request_id,sequence), UNIQUE(request_id,event_id));
CREATE TABLE IF NOT EXISTS gateway.signals(id text PRIMARY KEY, request_id uuid NOT NULL REFERENCES gateway.executions(request_id), kind text NOT NULL, payload jsonb NOT NULL, consumed boolean NOT NULL DEFAULT false, sequence bigserial, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS gateway.runtime_slot(id integer PRIMARY KEY CHECK(id=1), request_id uuid, owner text, unresolved boolean NOT NULL DEFAULT false);
INSERT INTO gateway.runtime_slot(id) VALUES(1) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS gateway.apply_retries(request_id uuid NOT NULL REFERENCES gateway.executions(request_id), original_run_id text NOT NULL, count integer NOT NULL DEFAULT 0 CHECK(count BETWEEN 0 AND 2), PRIMARY KEY(request_id,original_run_id));
GRANT USAGE ON SCHEMA gateway TO gateway_app;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA gateway TO gateway_app;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA gateway TO gateway_app;

