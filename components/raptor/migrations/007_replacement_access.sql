ALTER TABLE raptor.agent_attempt_access
 ADD COLUMN IF NOT EXISTS replacement_scope jsonb;
