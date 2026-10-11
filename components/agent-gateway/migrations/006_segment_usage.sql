ALTER TABLE gateway.attempts ADD COLUMN IF NOT EXISTS segment_usage jsonb;
