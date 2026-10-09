CREATE TABLE IF NOT EXISTS raptor.approval_submissions (
 request_id uuid NOT NULL,
 action_id text NOT NULL,
 binding jsonb NOT NULL,
 outcome text NOT NULL DEFAULT 'unknown' CHECK (outcome IN ('submitted','unknown','not_submitted')),
 provider_request_id text NOT NULL DEFAULT '',
 recorded boolean NOT NULL DEFAULT false,
 claimed_at timestamptz NOT NULL DEFAULT now(),
 recorded_at timestamptz,
 PRIMARY KEY(request_id,action_id),
 FOREIGN KEY(request_id,action_id) REFERENCES raptor.approvals(request_id,action_id)
);
GRANT SELECT,INSERT,UPDATE ON raptor.approval_submissions TO raptor_app;
