CREATE TABLE IF NOT EXISTS raptor.fleet_mutations (
 env_code text NOT NULL,
 group_id text NOT NULL,
 token text NOT NULL,
 intent jsonb NOT NULL,
 recorded boolean NOT NULL DEFAULT false,
 claimed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(env_code,group_id)
);
GRANT SELECT,INSERT,UPDATE,DELETE ON raptor.fleet_mutations TO raptor_app;
