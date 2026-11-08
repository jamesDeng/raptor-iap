CREATE TABLE IF NOT EXISTS raptor.pgcat_observations (
    request_id uuid NOT NULL REFERENCES raptor.requests(id),
    attempt_id uuid NOT NULL,
    scope_digest text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('hold', 'sql', 'drain', 'probe-release', 'traffic', 'final-traffic')),
    subject text NOT NULL,
    observed_at timestamptz NOT NULL,
    data jsonb NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    PRIMARY KEY(request_id, kind, subject),
    FOREIGN KEY(request_id, attempt_id) REFERENCES raptor.agent_attempt_access(request_id, attempt_id)
);
GRANT SELECT, INSERT, UPDATE ON raptor.pgcat_observations TO raptor_app;
