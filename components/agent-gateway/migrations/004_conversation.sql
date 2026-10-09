CREATE TABLE IF NOT EXISTS gateway.conversation_sessions(
 request_id uuid PRIMARY KEY REFERENCES gateway.executions(request_id), session_id text NOT NULL DEFAULT '', session_file text NOT NULL DEFAULT '',
 checkpoint jsonb, last_input_sequence bigint NOT NULL DEFAULT 0, input_open boolean NOT NULL DEFAULT false, attempt_id uuid,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS gateway.conversation_messages(
 request_id uuid NOT NULL REFERENCES gateway.executions(request_id),message_id uuid NOT NULL,actor_id uuid NOT NULL,input_sequence bigint NOT NULL CHECK(input_sequence>0),
 text text NOT NULL,text_hash text NOT NULL,accepted_at timestamptz NOT NULL,status text NOT NULL CHECK(status IN ('queued','delivered','answered','interrupted','rejected')),
 attempt_id uuid,reason text NOT NULL DEFAULT '',PRIMARY KEY(request_id,message_id),UNIQUE(request_id,input_sequence)
);
CREATE TABLE IF NOT EXISTS gateway.conversation_turns(
 request_id uuid NOT NULL REFERENCES gateway.executions(request_id),turn_id uuid NOT NULL,attempt_id uuid NOT NULL REFERENCES gateway.attempts(id),
 payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(request_id,turn_id)
);
GRANT SELECT,INSERT,UPDATE ON gateway.conversation_sessions,gateway.conversation_messages,gateway.conversation_turns TO gateway_app;
