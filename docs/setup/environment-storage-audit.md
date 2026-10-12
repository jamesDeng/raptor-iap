# Structured environment storage audit

Status: local code and migration test reviewed 2026-10-12; deployed data inventory is pending. Do not infer that the live database contains no unrecognized keys.

## Local evidence

- `components/raptor/migrations/008_structured_environments.sql` copies recognized scalar keys to columns, Git repository fields to `environment_repositories`, and remaining JSON values to `environment_settings`, then drops `config`.
- `components/raptor/internal/catalog/environments.go` reconstructs `config` for older readers. Direct source consumers found in this checkout include `components/raptor/internal/agentaccess/service.go` and `components/raptor/internal/openapi/agent_scope.go`.
- `go test ./internal/db -run TestStructuredEnvironmentMigrationPreservesEveryKey -count=1` passed against a disposable local PostgreSQL cluster. It covers nested unknown data, an empty string, and a recognized key with a non-string value. Catalog round-trip tests also passed.

## Read-only deployed audit before declaring migration complete

1. Confirm the intended rdev database and schema version through the approved read-only access path. Do not print secrets or connection strings.
2. Count environment rows and repository/settings rows. Inventory distinct `environment_settings.key` values and types (`jsonb_typeof(value_json::jsonb)`) with counts, not raw values.
3. Inspect each environment's nonsecret named columns, purpose/URL/base-branch/directory metadata, and synthesized API `config` response. Compare with a pre-migration backup or approved snapshot if available; without one, record that a full before/after proof is unavailable.
4. Check for repository rows with empty base branch or directory, duplicate purpose/URL/directory, and the same URL with conflicting base branches. Resolve legacy exceptions before attaching every repository to a sandbox.
5. Search all deployed readers outside this checkout before treating the JSONB column retirement as complete. Record query time, deployment revision, row counts, key counts, and any discrepancies in a separate redacted evidence file.
