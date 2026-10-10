CREATE TABLE IF NOT EXISTS gateway.operation_sessions (
 request_id uuid PRIMARY KEY REFERENCES gateway.executions(request_id),
 attempt_id uuid NOT NULL REFERENCES gateway.attempts(id),
 checkpoint jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
GRANT SELECT, INSERT, UPDATE ON gateway.operation_sessions TO gateway_app;
