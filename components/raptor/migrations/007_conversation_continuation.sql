-- Persist the completed attempt being continued so delayed terminal snapshots
-- remain fenced even after the input receipt becomes answered or interrupted.
ALTER TABLE raptor.request_messages ADD COLUMN IF NOT EXISTS continuation_attempt_id uuid;
