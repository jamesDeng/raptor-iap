ALTER TABLE raptor.requests ADD COLUMN IF NOT EXISTS approval_resume_pending boolean NOT NULL DEFAULT false;
ALTER TABLE raptor.requests ADD COLUMN IF NOT EXISTS approval_resume_at timestamptz;
