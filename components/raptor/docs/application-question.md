# Application question: Raptor integration contract

This branch supplies the Raptor request/UI side with offline acceptance. It does not establish live Gateway/Pi execution or request-bound authorization. Agent access issuance/introspection is pending the owner's token-mechanism decision. Do not enable live runtime using broad service credentials.

## Canonical input

Use the existing agent request definition: `type`, `object:{kind,code}`, `envCode`, `operations:[{name:"application.question",parameters:{question}}]`, resolved `skills:{tag,commitSha}`, and top-level `model:"gpt-5.6-luna"`. The legacy singular `operation/parameters` shape proposed in the Gateway session plan is not the stored agent definition. Gateway must consume the existing operations array. Exactly one question operation is allowed; text is nonblank, valid UTF-8, at most 2,000 code points and 8,192 bytes. Object/environment must exist. Skills remain pinned for the request; only cancellation is supported for this new operation.

Dispatch retains PUT request ID/type and its transactional outbox. Gateway owns attempts. Raptor owns only its schema.

## Public execution projection

Existing Gateway GET request `data` is projected onto: requestId, attemptId, status, runtimeMode, stage, appliedSkills, recoveryNeeded, updatedAt, result, checkpointStatus, cleanupStatus, failureCode and legacy cleanup. Unknown execution fields and private checkpoint paths are omitted. Prefer authoritative updatedAt on every snapshot. A row lock serializes cache/status updates; stale snapshots and regressions after terminal completion cannot erase the public answer.

Result: requestId, attemptId, selectedModel, actualModel, nonempty answer (16 KiB max), evidence (32 max), generatedAt and optional inputTokens/outputTokens usage. Live completion requires the selected/actual model match the request, verified checkpoint, confirmed cleanup, no recovery-needed state, and live timestamped Raptor request_get plus Infra cloud_identity_get/deployments_list/deployment_status_get references. Raptor context evidence identifies selected envCode/objectCode; it proves a live context read, not workload health. Status evidence binds appCode/envCode/clusterId/namespace/name/UID. An unhealthy point-in-time finding can be a successful inspection.

Gateway and Raptor must agree server identifiers `raptor` and `infra`, outcome strings `verified`/`confirmed`, and structured identity/state bounds before integration. These are the implemented Raptor consumer contract, not evidence that Gateway supports them yet. Sandbox and key absence plus attempt-access revocation are Gateway's prerequisites for confirmed cleanup. Cancel acknowledgment is not terminal cancellation.

Raptor caches validated public view/result and last-successful-sync time in `raptor.execution_views`. During failed/invalid/stale sync, UI shows last saved history. Generated answer and checkpoint/cleanup status render separately. Answer uses textContent; no HTML or raw Markdown execution.

## Event synchronization

Gateway supplies request-global, contiguous sequence across attempts, eventId, requestId, attemptId, occurredAt, kind, summary, structured details and explicit live/simulated evidence. Keep existing GET events envelope. Page limit is 200 for compatibility with current producer. Sort incoming batches, lock the Raptor request, compare duplicate content/identity, enforce no source sequence gaps, and commit the batch atomically before advancing the cursor. Live events require source timestamp and request/attempt identity. Simulated historical fixtures retain compatibility with missing source identity/time.

Browser timeline has its own Raptor sequence cursor; never substitute the Gateway source cursor. Unknown evidence is not labelled live or simulated. Imported details and answer must already be sanitized by Gateway; it must not send credentials or raw runtime logs.

## Migrations and tests

The owner migration runner applies embedded SQL files in lexical order under its advisory lock. Migrations are replay-safe. 002 adds global catalog code uniqueness, source attempt/sequence constraints and public view cache. Duplicate existing catalog/source sequences fail migration rather than silently rewriting identity. Run on a disposable local PostgreSQL cluster first; this session has not applied it to platform RDS.

From `components/raptor`, use a localhost-only TEST_DATABASE_URL and run `go test ./...`, `go vet ./...`, and `node --test web/poc/question.test.mjs ../../tests/harness/go-platform-web.test.mjs`. Do not use live cloud databases for testutil: it creates/drops test databases and test roles.

The local browser fixture verifies form submission, delayed progress, answer and desktop/narrow layout. Mock observations and model names are fixture data. Live browser acceptance, grants, image publishing, shared release configuration and rollout belong to the integration owner after authorization and contract agreement.
