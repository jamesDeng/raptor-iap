CREATE TABLE IF NOT EXISTS raptor.request_messages(
 request_id uuid NOT NULL REFERENCES raptor.requests(id), message_id uuid NOT NULL, actor_id uuid NOT NULL,
 input_sequence bigint NOT NULL CHECK(input_sequence>0), text text NOT NULL, text_hash text NOT NULL,
 status text NOT NULL DEFAULT 'accepted' CHECK(status IN ('accepted','queued','delivered','answered','interrupted','rejected')),
 reason text NOT NULL DEFAULT '', accepted_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(request_id,message_id), UNIQUE(request_id,input_sequence)
);
GRANT SELECT,INSERT,UPDATE ON raptor.request_messages TO raptor_app;
