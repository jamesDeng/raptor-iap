-- Readiness waits never contain an approval or authorize a gated mutation.
ALTER TABLE gateway.live_waits ALTER COLUMN approval_id DROP NOT NULL;
