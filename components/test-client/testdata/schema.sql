-- Dedicated test database only. Apply through the reviewed initialization path.
CREATE TABLE infra_test_operations(id text PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now());
