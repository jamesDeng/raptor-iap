CREATE TABLE IF NOT EXISTS gateway.live_waits (
 attempt_id uuid PRIMARY KEY REFERENCES gateway.attempts(id),
 request_id uuid NOT NULL REFERENCES gateway.executions(request_id),
 approval_id uuid NOT NULL,
 action_id text NOT NULL,
 wait jsonb NOT NULL,
 checkpoint jsonb NOT NULL,
 phase text NOT NULL DEFAULT 'checkpointed',
 UNIQUE(request_id,action_id)
);
GRANT SELECT, INSERT, UPDATE ON gateway.live_waits TO gateway_app;

ALTER TABLE gateway.executions ADD COLUMN IF NOT EXISTS resume_context jsonb;
