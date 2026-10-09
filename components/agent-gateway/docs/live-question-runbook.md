# Gateway/Pi application.question rollout package

## Status and ownership

This branch implements an offline Gateway/Pi consumer slice. No cloud compatibility run, provider inference, deployment, RAM grant or shared merge was performed. Fixture `evidenceMode:live` exercises the wire contract; it is not independently observed cloud evidence. Existing PR29 MCP acceptance does not prove this agent slice.

This integration includes Raptor, Gateway, harness, Infra read observations and Helm wiring. Local fixture acceptance is recorded in the coordinator ledger. Cloud compatibility, inference and deployed acceptance remain separate gates; keep live mode disabled until those prerequisites pass.

## Integrated contracts

1. Service-authenticated `GET /v1/requests/{requestId}/context` returns an immutable definition with `type:"agent"`, top-level `model`, application object, `envCode`, one `operations` entry named `application.question` with `parameters.question`, and pinned `skills.commitSha`. Context also supplies the canonical `definitionSha256` and environment `config.ackClusterId`.
2. `POST /v1/requests/{requestId}/agent-access` returns HTTP 201 with an opaque credential and a bounded expiry, exact trusted AttemptBinding (including definition hash and configured cluster), and configured MCP URLs. DELETE for that attempt confirms idempotent revocation.
3. Raptor MCP uses `Authorization: Bearer <opaque attempt token>` and exposes request_get, environment_get and object_get. Raptor enforces attempt scope, expiry and revocation. Infra MCP uses separate `X-Infra-Authorization: Basic <encoded service credential>` and exposes cloud_identity_get, deployments_list and deployment_status_get in this read-only deployment. **Infra Basic authentication is service-wide, not request-scoped; cancellation does not revoke it.** The harness validates requested selectors and returned evidence against its trusted binding, but this does not turn Basic Auth into server-enforced attempt authorization.
4. Successful Infra reads supply UTC RFC3339 `observedAt`, `evidenceMode:"live"` and checked environment/object/resource identity. Status verifies UID against the request; the harness additionally matches discovery. Collection time does not prove metric freshness. Raptor context is catalog evidence.

The definition fingerprint is SHA-256 of Go JSON encoding after decoding the definition into JSON values (sorted map keys). It hashes the definition only, not the execution envelope. Producer fixture tests exercise agreement with Raptor's immutable definition.

## Runtime and credentials

`GATEWAY_RUNTIME_MODE=disabled|simulated|live` is explicit. Unset mode is disabled unless legacy `GATEWAY_SIMULATION=true`; conflicting settings fail startup. New executions report configured mode; historical records retain their recorded mode.

Live configuration file comes from proposed Secret `gateway-live-config`, key `config.json`, mounted as a private regular file (0600) at `/run/raptor/live/config.json`; set `GATEWAY_LIVE_CONFIG_FILE`. The controller credential comes from separate proposed Secret `gateway-live-controller`, key `credential.json`, at `/run/raptor/live/credential.json`; set `GATEWAY_CONTROLLER_CREDENTIAL_FILE`. The loader rejects symlinks, world/group permissions, unknown fields, trailing JSON and files over 64 KiB. Default Kubernetes projected Secret symlinks need a reviewed private-file delivery mechanism, such as an init copy owned by UID 10001. A subPath file also needs owner-readable 0600 permissions for UID 10001; root-owned 0600 files are unreadable by the image user. Controller credential requires explicit temporary accessKeyId/accessKeySecret/securityToken; no profile discovery or sandbox injection. Rotation/restart procedure is coordinator-owned.

The approved private ACK service wiring uses HTTP inside the cluster for backend and Gateway calls. `RAPTOR_CLUSTER_HTTP=true` explicitly enables only the exact fixed origins `http://raptor-backend:8871` and `http://agent-gateway:8874`; other remote HTTP remains rejected. These internal credentials are not encrypted in transit. External MCP and Gateway-to-Raptor calls continue to require HTTPS. Adding service-to-service TLS is future work.

Existing SERVICE_USERNAME/SERVICE_PASSWORD authenticate Gateway APIs and Raptor controller calls only. GATEWAY_DATABASE_URL is the Gateway role only; MIGRATION_DATABASE_URL is schema-owner migration only. No operator profile, RDS admin or Infra deployment credential enters the sandbox.

LiveConfig JSON keys: infraUsername, infraPassword, accountId, region (ap-southeast-1), teamId, templateId, bucket, bucketPrefix, volumeName, volumeId, executionRoleArn, bootstrapCheckpoint, raptorMcpUrl, infraMcpUrl, harnessDir (`/opt/raptor-harness`). Bootstrap descriptor uses archiveKey/checksumKey/sha256/bytes/piVersion/encryption/verifiedAt. Startup independently verifies the archive, checksum, sizes and remote AES256 encryption; a supplied verifiedAt is not proof.

Template prerequisites: existing Singapore sandbox team/template/OSS Volume; Node exactly 22.23.3 and Python3 available to user. Preparation fails if absent. Pi coding-agent and pi-mcp are pinned 0.99.2; npm ci uses the lockfile, with install scripts disabled. Harness files/config use umask 077. Attempt MCP credential/config/progress/logs live outside the auth archive root. The archive allowlists auth.json and session files; each request starts a fresh conversation. OAuth only, no API-key fallback.

Limits: one leased worker and durable slot, 600-second overall attempt, sandbox TTL no more than 900 seconds, 90-second model call, ten assistant turns, 1024 output tokens per response. Question <=2000 code points/8 KiB; answer <=16 KiB; terminal/progress <=64 KiB; checkpoint <=16 MiB. No inferred successful completion from answer existence.

## Controller permission proposal (review before granting)

This is an action/resource proposal, not an applied policy. The [FCSandbox RAM guide](https://www.alibabacloud.com/help/zh/functioncompute/configure-ram-user-permissions) documents Team-scoped API-key resources and explicitly says individual keys and individual sandbox IDs cannot be RAM scoped. E2B data-plane calls use a Team key, independently of controller RAM actions.

| Principal | Actions consumed | Proposed resource | Remaining check |
|---|---|---|---|
| Gateway controller | fcsandbox:CreateApiKey, ListApiKeys, UpdateApiKey, DeleteApiKey | acs:fcsandbox:ap-southeast-1:ACCOUNT:teams/TEAM/apikeys/* and required collection ARN | Verify each operation's authorization table and list collection ARN with deny tests; no single-key RAM isolation |
| Gateway controller | fcsandbox:GetVolume | acs:fcsandbox:ap-southeast-1:ACCOUNT:teams/TEAM/volumes/VOLUME | Verify existing Volume's account/team/role/bucket/prefix/endpoint/read-write status |
| Gateway controller | oss:GetBucketEncryption | acs:oss:*:ACCOUNT:BUCKET | Verify bucket-level action support on existing role |
| Gateway controller | oss:GetObject (including metadata HEAD) | acs:oss:*:ACCOUNT:BUCKET/PREFIX/* | Read-only remote verification, no controller archive write/delete |
| Existing sandbox execution role | Existing OSS mount read/write permissions for PREFIX | Existing reviewed mount policy | Do not broaden/create here; verify mount does not expose other prefixes |
| Ephemeral sandbox key | E2B create/connect/files/process/inventory/kill | Team key; application attempt metadata is software ownership filter | Template/individual sandbox scoping is not supplied by this adapter; cross-key same-Team recovery must be tested |

No Team, template, Volume, quota or bucket creation permission is needed. Never substitute wildcard administrator access when a deny test fails. Proposed ephemeral keys expire within attempt deadline; reconciliation uses a separately journaled short-lived key and must confirm removal of every owned key.

## Build and migration

Apply additive migrations 001 and 002 through `gateway -migrate` using schema-owner credentials before deploying the app role. Migration scope is gateway schema only. Review SQL against the coordinator's migration ownership procedure.

Gateway Dockerfile now consumes explicit BuildKit named context `agent-harness`. From components/agent-gateway: `docker build --build-context agent-harness=../agent-harness .`. The shared platform workflow supplies this named source context and publishes only after integrated tests pass. Only an explicit source allowlist is copied; no node_modules, OAuth state, private config or logs. Docker was unavailable in this session: image build and immutable image digest are unverified. Coordinator builds, scans, records registry digest and deploys it; do not deploy a mutable tag or claim a digest from source hashes.

## Offline evidence and limitations

Gateway tests use disposable localhost PostgreSQL and TLS fixtures. They exercise intent fencing, binding immutability, contiguous/deduplicated events, bounded progress/terminal decoding, cleanup-blocked answer retention, lost lease, cancellation, restart without inference replay, known-ID inventory omission, encrypted remote checkpoint verification and native transport frames. Harness tests use actual pinned embedded Pi/MCP SDK with synthetic provider/transport fixtures, two fresh sessions and scoped catalogs. They perform no live provider/model network calls. Go vet passes. Review regressions also cover lease loss during revocation, cancellation during checkpointing, unbound-claim restart, checkpoint durability before cleanup, and a status response for a different discovered deployment. The compatibility CLI dry default reports zero cloud/model calls.

These layered fixture tests do not prove vendor protocol compatibility, cross-key recovery, real OAuth refresh, a full deployed browser-to-Gateway-to-harness request, or live request/attempt authorization. The coordinator must verify those separately.

## Authorized compatibility and acceptance procedure

1. Review/freeze the shared contracts, policy proposal, private credential delivery, existing Volume/template and build context; record immutable image digest. Keep runtime disabled until Raptor attempt scope deny tests and runtime compatibility pass. Infra remains separately authenticated by its read-only Basic principal, with the accepted service-wide limitation.
2. Separately authorize cloud compatibility. CLI default `sandbox-compat` creates nothing. `sandbox-compat -execute -request-id UUID` requires the private config, Gateway-role database and Raptor service configuration. It exclusively leases the journal and consumes only the explicitly selected queued compatibility request; it refuses an unrelated earlier queued request or active slot. Create a dedicated compatibility request through the existing coordinator flow. No inference is launched. Its Gateway history ends failed/Interrupted because compatibility is not an answered application question; assess the sanitized compatibility report separately.
3. Compatibility exercises key/create/connect/private file/fixed command/pinned runtime/remote encrypted OSS mount readback/termination/key absence. Inspect journal if interrupted. Probe object uses PREFIX/lifecycle/compat-ATTEMPT and is removed after verified readback; a crash may leave a harmless probe object requiring coordinator cleanup. Stop if new same-Team key cannot connect/terminate an old sandbox. Never launch a replacement while ownership is unresolved.
4. After separate deployment authorization, enable live mode; verify GET execution and browser mode labels. Submit selected application question “Is agent-gateway healthy in rdev.ali?”. Capture progress before terminal, exact selected/actual model, catalog context, live identity/discovery/status time and UID, generated answer, verified encrypted remote checkpoint and confirmed sandbox/key/access cleanup.
5. An unhealthy point-in-time answer is valid success. NeedsSignIn, missing/invalid evidence, changed UID, timeout, checkpoint failure and unknown cleanup must remain distinct. Cleanup uncertainty retains slot and marks blocked; answer remains visible separately. An unknown create with no persisted resource ID and no uniquely attributable inventory result remains blocked; an empty inventory alone never resolves it. Coordinator investigation/authoritative resolution is required rather than automatic replacement.
6. Submit a second request: fresh sandbox/conversation, renewable auth reused or truthful NeedsSignIn. Restart Gateway with old history and an authorized interrupted attempt. Confirm no inference replay and cleanup through a temporary reconciliation key. Capture costs/balance before and after using coordinator overnight procedure.

Operational skills are not loaded in this read-only question slice: the commit is an immutable catalog/binding identifier, not evidence of skill execution. The model uses a fixed system prompt and six scoped reads. Loading operational skills requires a separate reviewed contract.

No live acceptance success is claimed by this branch.

### Public progress narration

The harness asks the selected agent to provide a brief public description before each tool round. Completed assistant text from a `toolUse` message is emitted as `progress/running` with optional `summary`; thinking blocks and the final answer are excluded. This is paragraph-level progress, not token-level streaming. A model that emits no public pre-tool text still produces its normal tool events; the UI does not invent commentary.

The harness drops public paragraphs exceeding 2,048 bytes instead of truncating them, preserving complete text for secret matching. Both the writer and Gateway enforce the same byte limit. Runtime polling redacts per-attempt credentials (and the native runtime key), and Gateway applies its existing secret redaction before storing either the runtime journal or public event. The existing request-bound WebSocket delivers these events, and assistant-ui displays their summaries inline with tool groups. No schema migration is needed for the existing JSON payloads.

Roll out Gateway before the updated harness: the older strict runtime decoder rejects the new summary field. Updated Gateway accepts old events without summary. The frontend already understands progress summaries. This change does not add skill execution, permission approval or PR operations to the current read-only application-question harness.

Verification uses the embedded Pi SDK with a deterministic local provider/MCP fixture (no model billing or cloud mutation), plus PostgreSQL journal/replay and existing stream regression tests. These tests do not constitute a production deployment or a new live execution.

## Request conversations (default off)

Deploy additive Raptor migration 005 and Gateway migration 004 through the
existing schema-owner migration flow. Deploy Gateway and its matching harness
before enabling `GATEWAY_CONVERSATION_ENABLED=true`; live runtime wiring must be
present. Then enable `RAPTOR_CONVERSATION_ENABLED=true`. Both flags default off.
Raptor checks Gateway capability version 1 before accepting input. Unsupported
runtime combinations and pre-feature completed Requests remain view only.

Browser messages go through Raptor login, CSRF and creator/admin authorization.
Gateway message/capability routes require service authentication. Public Gateway
ingress must continue to expose only the read-only progress WebSocket; do not
publish `/v1/requests/*/messages`. Chat never supplies approval or broadens the
six application.question read tools.

Messages are limited to 2,000 Unicode code points / 8,192 UTF-8 bytes and 20
outstanding per Request. Retry an HTTP timeout with the same message ID. A
Gateway receipt moves accepted → queued → delivered → answered; interrupted or
rejected input displays a reason. Delivered means Pi appended the user message,
not merely that a sandbox file was written. Ambiguous delivery after a crash is
interrupted rather than automatically replayed. A user may submit a new message
explicitly after recovery permits it.

Continuation requires that Request's exact Pi session association, verified
checkpoint and confirmed sandbox/key/access cleanup. It gets fresh Request
context, attempt ID, deadline and access. Each attempt retains the existing
90-second / 10-turn / 1,024-output-token limits. Input does not reset them.

Local verification uses disposable PostgreSQL, production HTTP/outbox/worker
code and the pinned Pi SDK with deterministic model/MCP fixtures. The local
fixture checkpoint and sandbox do not prove deployed AX/Native restore. Before
enabling these flags in an environment, review an acceptance receipt proving:
exact-session checkpoint restoration, new attempt credentials, browser ingress
and ticket renewal, cleanup, and interrupted-message recovery on that runtime.
Keep flags off until that deployed acceptance is reviewed. No provisioning is
required or performed by this feature.
