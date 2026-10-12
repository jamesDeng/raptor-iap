CREATE TABLE IF NOT EXISTS gateway.attempt_repositories (
 attempt_id uuid NOT NULL REFERENCES gateway.attempts(id),
 row_index integer NOT NULL CHECK (row_index>=0),
 environment_code text NOT NULL,
 purpose text NOT NULL,
 repository_url text NOT NULL,
 directory text NOT NULL,
 base_branch text NOT NULL,
 base_commit text NOT NULL,
 PRIMARY KEY(attempt_id,row_index)
);
GRANT SELECT,INSERT ON gateway.attempt_repositories TO gateway_app;
