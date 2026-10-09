CREATE TABLE IF NOT EXISTS gateway.progress_tickets (
 token_hash text PRIMARY KEY,
 request_id uuid NOT NULL REFERENCES gateway.executions(request_id),
 subject text NOT NULL,
 origin text NOT NULL,
 expires_at timestamptz NOT NULL
);
GRANT SELECT, INSERT, DELETE ON gateway.progress_tickets TO gateway_app;
