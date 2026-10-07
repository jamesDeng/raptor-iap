# Application question: Raptor integration contract

This branch supplies the Raptor request/UI side with offline acceptance. It does not establish live Gateway/Pi execution or request-bound authorization. The owner selected opaque tokens; Raptor issuance/introspection/revocation and the read-only agent MCP boundary are implemented with local acceptance. Do not enable live runtime using broad service credentials.

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

## Opaque attempt access

Controller-authenticated POST `/v1/requests/{requestId}/agent-access` accepts `{attemptId,definitionSha256,expiresAt}` and returns `{data:{credential,expiresAt,binding,raptorMcpUrl,infraMcpUrl}}`. Use `definitionSha256` from canonical controller context. It is SHA-256 over the normalized definition decoded as JSON and re-encoded with sorted object keys (the existing operations array/top-level model form). The request's resolved skills and model are immutable for this slice.

Binding contains requestId, attemptId, operation, objectKind, objectCode, envCode, skillsCommit, model, definitionSha256 and clusterId. URLs must be configured HTTPS origins without embedded credentials, query or fragment. Token is `raptor_at_` plus 32 random bytes in URL-safe base64. Only its SHA-256 is stored in `raptor.agent_tokens`; the raw credential is returned once and must remain in Gateway memory/private sandbox files.

ExpiresAt must be future and at most 600 seconds from Raptor's clock. The first issuance fixes the attempt's deadline. Reissuing for an active attempt rotates its token and immediately invalidates the old token, with no deadline extension. Revoked/expired attempts cannot be revived. DELETE `/v1/requests/{requestId}/agent-access/{attemptId}` writes a revocation tombstone even before issuance, preventing a delayed issue from escaping cancellation. Gateway must perform revocation as part of confirmed cleanup.

Private POST `/v1/agent-access/introspect` requires separate inspector Basic credentials and `{credential,audience,tool,selectors}`. It returns `{data:{active,binding?,expiresAt?}}` with no raw token. Inactive or denied scope has `active:false`; unavailable storage/config fails closed. No positive cache. Terminal/non-executable request state, cancellation intent, changed fingerprint or changed environment cluster invalidates access. Empty tool/selectors is only for MCP protocol/catalog authentication. Raptor tool selectors require requestId and the matching envCode or kind/code. Infra selectors require bound envCode and object/app code; status also requires matching cluster and nonempty namespace/name/UID. Infra must independently rediscover and validate that full target before provider reads; Raptor does not establish membership of an Infra workload.

Agent `Authorization: Bearer` is accepted only on POST `/mcp`, for initialize, initialized notification, ping, tools/list and calls to request_get/environment_get/object_get. Other tools/resources/prompts/methods and `/v1/` proxy access are denied. Each tool rechecks introspection and returns only the selected context. Agent environment configuration exposes only reviewed string fields ackClusterId, region and namespace. Read responses include requestId/attemptId, selected env/object, observedAt, evidenceMode=live and dataKind=catalog; this denotes an actual context read, not workload-health evidence. Missing introspection config or unavailable verifier denies agent execution.

Required private deployment configuration (integration owner only): backend `RAPTOR_AGENT_MCP_URL`, `INFRA_AGENT_MCP_URL`; backend and Open API `AGENT_INTROSPECTION_USERNAME`, `AGENT_INTROSPECTION_PASSWORD`; existing `SERVICE_USERNAME`/`SERVICE_PASSWORD` for controller calls. Keep inspector credentials distinct from controller/deployment credentials and out of Pi. The dedicated Open API introspection route admits only inspector credentials; they do not gain the service HTTP proxy. No credentials or grants were activated by this branch.

## Migrations and tests

The owner migration runner applies embedded SQL files in lexical order under its advisory lock. Migrations are replay-safe. 002 adds global catalog code uniqueness, source attempt/sequence constraints and public view cache; 003 adds hashed opaque tokens and immutable/revocable attempt grants. Duplicate existing catalog/source sequences fail migration rather than silently rewriting identity. Run on a disposable local PostgreSQL cluster first; this session has not applied it to platform RDS.

From `components/raptor`, use a localhost-only TEST_DATABASE_URL and run `go test ./...`, `go vet ./...`, and `node --test web/poc/question.test.mjs ../../tests/harness/go-platform-web.test.mjs`. Do not use live cloud databases for testutil: it creates/drops test databases and test roles.

The local browser fixture verifies form submission, delayed progress, answer and desktop/narrow layout. Mock observations and model names are fixture data. Live browser acceptance, grants, image publishing, shared release configuration and rollout belong to the integration owner after authorization and contract agreement.
