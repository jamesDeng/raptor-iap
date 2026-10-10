# Model Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let managers connect Codex, enable models, and let Request creators choose an enabled model that the bound runtime actually uses.

**Architecture:** Raptor owns encrypted provider credentials and model policy. Requests persist provider/model identity; Gateway leases an attempt-bound credential and Pi executes that exact model. The Codex adapter is first, with a provider interface for future Bedrock, Bailian, and OpenAI API adapters.

**Tech Stack:** Go 1.25+, PostgreSQL/pgx, Node 22.23.3, pinned Pi 0.99.2, Raptor HTML/JavaScript, Gateway Go runtime, existing Helm/GitOps deployment.

**Spec:** `docs/superpowers/specs/2026-10-10-model-providers-design.md`

## Global Constraints

- One admin-managed Codex connection serves private internal Requests; no per-user provider accounts.
- Tokens are encrypted before PostgreSQL storage; the key is private runtime configuration outside source, Terraform state, and the database.
- Browser responses, Request definitions, logs, progress, and returned artifacts never contain access or refresh tokens.
- Existing Requests keep their immutable definitions; do not import old checkpoint credentials implicitly.
- No silent provider/model fallback, no implicit enablement of newly discovered models, and no live rollout before supported authorization and account eligibility are verified.
- Runtime changes must preserve the current exclusive attempt lease, checkpoint fencing, and cleanup semantics. Raptor provider `codex` maps to Pi provider `openai-codex`, not Pi `openai`.
- User has authorized PR merge, deployment, and post-deployment testing. Do not infer authorization to provision unrelated cloud resources.

## Review Focus

- Expired device code: polling stops, no credential is replaced, and the UI reports expiry (Task 2).
- Concurrent refresh: only the newest credential generation is committed (Task 3).
- Catalog changes between form load and submit: Raptor rejects stale selections and the form reloads choices (Tasks 4 and 7).
- Disabled/disconnected model while queued: Gateway blocks dispatch without fallback; active cancellation is reported accurately (Tasks 5 and 6).
- Restored Pi checkpoint has older `auth.json`: current database credential wins without changing the Request session (Task 6).

---

## File map

- `components/raptor/internal/modelproviders/`: provider types, Codex connection adapter, encrypted store, model policy, browser/admin/service handlers.
- `components/raptor/migrations/008_model_providers.sql`: connections, credential ciphertext, catalog, policy, login sessions, audit.
- `components/raptor/internal/backend/server.go`: register provider service; `components/raptor/internal/requests/{service,schema}.go`: Request admission.
- `components/raptor/internal/admin/{server,templates}.go`: manager UI; `components/raptor/web/poc/{app.js,index.html}`: Request selector.
- `components/agent-gateway/internal/execution/{live_models,live_worker}.go`: bound provider/model admission; `components/agent-gateway/internal/runtime/live.go`: credential injection and refresh publication.
- `components/agent-harness/{live-contract,live-question,pi-adapter}.mjs`: exact model selection and protected auth state.
- `components/raptor/internal/frontend/server.go`, Helm chart/release manifests, and private runtime configuration: route isolation and deployment.

### Task 1: Confirm supported Codex login path and deployment eligibility

**Files:** Create `docs/setup/model-provider-auth-qualification.md`; inspect pinned Pi package and current official OpenAI guidance.

**Interfaces:** Produces a documented `supported=true/false` go/no-go receipt for device-code authorization and private hosted use. No real token is captured in this task.

- [ ] Read the pinned Pi 0.99.2 public login interface and prove a headless device-code flow can be started, polled, cancelled, and expired without invoking private token-exchange internals.
- [ ] Verify whether the intended private hosted, shared-account use is eligible through the supported OpenAI path; record source URLs, scope/permission requirements, and the exact unresolved blocker if eligibility cannot be established.
- [ ] Write the qualification receipt with a synthetic protocol fixture; reject live activation when either requirement is false.
- [ ] Run the fixture and link its output in the receipt; commit only the nonsecret receipt and fixture.

### Task 2: Provider domain and one-use authorization sessions

**Files:** Create `components/raptor/internal/modelproviders/{types,auth_sessions,codex}.go` and focused tests; create `components/raptor/migrations/008_model_providers.sql`.

**Interfaces:** `type ProviderID string`, `type ModelID string`, `type ConnectionStatus string`; `StartCodexConnect(ctx, adminID string) (ConnectChallenge, error)`, `PollCodexConnect(ctx, adminID, sessionID string) (ConnectStatus, error)`, `CancelCodexConnect(ctx, adminID, sessionID string) error`. `ConnectChallenge` has `SessionID`, `VerificationURL`, `UserCode`, `ExpiresAt`; no tokens.

- [ ] Write failing tests for admin/session binding, single use, cancellation, expiry, and old-credential preservation after failed sign-in.
- [ ] Run `go test ./internal/modelproviders -run 'TestConnect' -count=1` from `components/raptor`; expect failure.
- [ ] Add the migration and minimal Codex adapter/session implementation; keep provider protocol behind an interface and use only the supported path qualified by Task 1.
- [ ] Rerun the focused tests; require pass. Commit migration, adapter, and tests.

### Task 3: Encrypted credential store and refresh serialization

**Files:** Create `components/raptor/internal/modelproviders/{crypto,store,refresh}.go` and tests; modify `components/raptor/cmd/backend/main.go` for a required private encryption-key source when feature enabled.

**Interfaces:** `EncryptCredential(plaintext []byte, key []byte, keyVersion string) (Ciphertext, error)`; `LoadCredential(ctx, providerID ProviderID) (CredentialRecord, error)`; `ReplaceCredential(ctx, providerID ProviderID, expectedGeneration int64, next []byte) (CredentialRecord, error)`. A mismatch returns `ErrStaleGeneration`.

- [ ] Write failing tests for ciphertext round-trip, wrong key, plaintext absence from rows/API errors, concurrent generation conflict, and refresh failure retaining the prior record.
- [ ] Run `go test ./internal/modelproviders -run 'TestCredential|TestRefresh' -count=1`; expect failure.
- [ ] Implement authenticated encryption with random nonce, key version, and generation compare-and-swap in one transaction; restrict SQL grants on credential tables.
- [ ] Rerun focused tests and a PostgreSQL integration test against disposable DB; require pass. Commit.

### Task 4: Catalog, policy, admin API, and public enabled-model API

**Files:** Create `components/raptor/internal/modelproviders/{catalog,policy,http}.go` and tests; modify `components/raptor/internal/backend/server.go` and `components/raptor/internal/frontend/server.go`.

**Interfaces:** `ListEnabled(ctx) ([]EnabledModel, error)`; `SetPolicy(ctx, adminID string, providerID ProviderID, expectedVersion int64, enabled []ModelID, defaultID ModelID) error`; `ValidateSelection(ctx, providerID ProviderID, modelID ModelID) (connectionVersion int64, error)`. Browser API paths and private service paths are fixed in the spec.

- [ ] Write failing HTTP/service tests for non-admin denial, CSRF denial, unknown model, conflicting policy version, discovery failure preserving policy, disabled model exclusion, and private route denial at public frontend.
- [ ] Run `go test ./internal/modelproviders ./internal/frontend -count=1`; expect the new tests to fail.
- [ ] Implement nonsecret admin/read APIs, explicit enablement, status/audit writes, and frontend management-route isolation.
- [ ] Rerun tests; require pass. Commit.

### Task 5: Persist and dispatch selected provider/model

**Files:** Modify `components/raptor/internal/domain/models.go`, `components/raptor/internal/requests/{service,schema}.go`, and request tests; modify `components/agent-gateway/internal/execution/{live_models,live_worker}.go` and tests.

**Interfaces:** `RequestInput.ProviderID string`, existing `RequestInput.Model string`, and immutable `RequestInput.ConnectionVersion int64` populated by Raptor after `ValidateSelection`; `AttemptBinding.ProviderID string`, `AttemptBinding.ConnectionVersion int64`. Gateway validates the exact pair before starting a live attempt. Credential generation is separate and may change during token refresh.

- [ ] Write failing Raptor tests for enabled selection, stale/disabled selection, idempotent retry, and immutable stored pair/connection version.
- [ ] Run `go test ./internal/requests -run 'Test.*Model' -count=1`; expect failure.
- [ ] Implement admission and outbox serialization without accepting browser-supplied connection version.
- [ ] Write failing Gateway tests for wrong provider/model/connection version, queued disconnect, and no fallback.
- [ ] Implement Gateway binding validation against the provider lease result; run both focused Go suites and require pass. Commit.

### Task 6: Attempt-bound credential lease and Pi execution

**Files:** Create `components/raptor/internal/modelproviders/service_http.go` and tests; modify `components/agent-gateway/internal/runtime/live.go` and tests; modify `components/agent-harness/{live-contract,live-question,pi-adapter}.mjs` and tests.

**Interfaces:** `POST /internal/v1/model-credentials/lease` accepts `requestId`, `attemptId`, `providerId`, `modelId`, `connectionVersion`; its service-authenticated private response carries the current credential and credential generation over verified TLS, with `Cache-Control: no-store`. `POST /internal/v1/model-credentials/refresh` accepts the same binding, expected credential generation, and rotated credential. Pi's binding includes `providerId` and exact `modelId`.

- [ ] Write failing service tests for forged Request/attempt, expired lease, revoked connection, replay, and cross-provider access.
- [ ] Write failing runtime/harness tests for exact model, old checkpoint auth replacement, refresh publication before success, crash uncertainty, and token redaction from every public result.
- [ ] Run `go test ./internal/modelproviders ./internal/runtime ./internal/execution -count=1` in their respective modules and `node --test components/agent-harness/*.test.mjs`; expect the new assertions to fail.
- [ ] Implement authenticated lease exchange, sandbox-local credential setup, database-first refresh publication, and result validation; preserve session files and checkpoint fencing.
- [ ] Rerun focused suites and the existing conversation acceptance fixture; require pass. Commit.

### Task 7: Admin and Request user interfaces

**Files:** Modify `components/raptor/internal/admin/{server,templates}.go`, `components/raptor/web/poc/{index.html,app.js}`, and focused UI tests in those directories.

**Interfaces:** Admin consumes Task 4 admin APIs and Task 2 connect status. Request form consumes `GET /api/v1/model-providers`; submits `providerId` and `model` IDs.

- [ ] Write failing UI tests for device code pending/expiry, connected status without token display, model policy update, no enabled models, stale selection refresh, and model choice in submitted Request.
- [ ] Run `go test ./internal/admin -count=1` and `node --test web/poc/*.test.mjs` from `components/raptor`; expect new tests to fail.
- [ ] Implement provider cards, device-code panel, discovered/enabled controls, and dynamic Request selector; show requested/actual provider/model in saved views.
- [ ] Rerun focused UI tests and browser smoke tests against a fixture backend; require pass. Commit.

### Task 8: Integration, PR, deployment, and live acceptance

**Files:** Modify component deployment values/manifests and `docs/setup/model-providers-acceptance.md`; create sanitized acceptance receipt. Keep all secret material outside Git and Terraform state.

**Interfaces:** Feature flag defaults off until compatible backend, Gateway, and harness versions are deployed. No new Alibaba Cloud resource is created by this plan.

- [ ] Run Raptor, Gateway, and harness full local test suites with disposable PostgreSQL; record exact commands/results, including authorization and credential-redaction checks.
- [ ] Build images and exercise an end-to-end fixture Request through Raptor → Gateway → Pi with the selected provider/model; verify encrypted DB row, attempt binding, requested/actual model, checkpoint, and cleanup.
- [ ] Open a PR containing only this feature; review the diff and CI results. Merge it under the user's standing authorization once required checks pass.
- [ ] Deploy through the repository's existing reviewed GitOps release flow; verify exact image revisions, migration success, health, and unchanged unrelated infrastructure. Do not bypass any Terraform plan/comment or apply gates if deployment changes Terraform.
- [ ] Run deployed browser/API checks for admin-only access, CSRF, policy, disabled model, and Request selection. If Task 1 established eligibility and the owner completes the account authorization, run a real selected-model Request and subsequent refresh/restart acceptance. Record credentials only in the protected database, and publish only sanitized evidence.
- [ ] Keep the feature flag off if live authorization, token refresh, checkpoint integrity, or cleanup is unverified. Record the exact blocker and safe deployed state; do not claim live success from fixtures.
