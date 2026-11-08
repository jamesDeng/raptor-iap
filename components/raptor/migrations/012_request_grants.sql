CREATE TABLE IF NOT EXISTS raptor.request_grants (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL REFERENCES raptor.requests(id),
    env_code text NOT NULL,
    scope_digest text NOT NULL,
    bundle_digest text NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    capabilities jsonb NOT NULL CHECK (jsonb_typeof(capabilities) = 'array'),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'denied')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    decided_at timestamptz,
    decided_by uuid,
    UNIQUE(request_id, id),
    UNIQUE(request_id, bundle_digest)
);
CREATE UNIQUE INDEX IF NOT EXISTS request_grants_one_pending
    ON raptor.request_grants(request_id) WHERE state = 'pending';

CREATE TABLE IF NOT EXISTS raptor.request_grant_actions (
    request_id uuid NOT NULL REFERENCES raptor.requests(id),
    grant_id uuid NOT NULL,
    action_id text NOT NULL,
    input_digest text NOT NULL,
    capability jsonb NOT NULL CHECK (jsonb_typeof(capability) = 'object'),
    arguments jsonb NOT NULL CHECK (jsonb_typeof(arguments) = 'object'),
    state text NOT NULL DEFAULT 'claimed' CHECK (state IN ('claimed', 'dispatching', 'submitted', 'not_submitted', 'unknown')),
    provider_request_id text NOT NULL DEFAULT '',
    claimed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    recorded_at timestamptz,
    PRIMARY KEY(request_id, action_id),
    FOREIGN KEY(request_id, grant_id) REFERENCES raptor.request_grants(request_id, id)
);
GRANT SELECT, INSERT, UPDATE ON raptor.request_grants, raptor.request_grant_actions TO raptor_app;
