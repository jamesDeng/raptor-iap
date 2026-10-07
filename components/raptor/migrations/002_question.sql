-- Fail on duplicate recovered codes; never silently rewrite catalog identity.
CREATE UNIQUE INDEX IF NOT EXISTS objects_global_code ON raptor.objects(code);
ALTER TABLE raptor.events ADD COLUMN IF NOT EXISTS attempt_id text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS raptor.execution_views(request_id uuid PRIMARY KEY REFERENCES raptor.requests(id), public_view jsonb NOT NULL, synced_at timestamptz NOT NULL);
GRANT SELECT,INSERT,UPDATE,DELETE ON raptor.execution_views TO raptor_app;
CREATE UNIQUE INDEX IF NOT EXISTS events_source_sequence ON raptor.events(request_id,source_sequence) WHERE source_sequence IS NOT NULL;
