ALTER TABLE raptor.pull_requests ADD COLUMN IF NOT EXISTS attempt_id uuid;
ALTER TABLE raptor.pull_requests ADD COLUMN IF NOT EXISTS base_commit text;
ALTER TABLE raptor.pull_requests ADD COLUMN IF NOT EXISTS base_branch text;
ALTER TABLE raptor.pull_requests ADD COLUMN IF NOT EXISTS working_branch text;
