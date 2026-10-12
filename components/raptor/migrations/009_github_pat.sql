CREATE TABLE IF NOT EXISTS raptor.github_pat_setting (
 id integer PRIMARY KEY CHECK (id=1),
 secret_ref text NOT NULL,
 owner_label text NOT NULL,
 verified_at timestamptz NOT NULL,
 updated_by text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
GRANT SELECT,INSERT,UPDATE,DELETE ON raptor.github_pat_setting TO raptor_app;
CREATE TABLE IF NOT EXISTS raptor.github_pat_cleanup (
 secret_ref text PRIMARY KEY,
 queued_at timestamptz NOT NULL DEFAULT now()
);
GRANT SELECT,INSERT,DELETE ON raptor.github_pat_cleanup TO raptor_app;
