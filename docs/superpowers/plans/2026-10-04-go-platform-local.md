# Local Go Raptor and Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the first local Go/PostgreSQL platform with catalog, structured requests, durable/live progress, approvals, PR events and skills switching against labelled simulated execution.

**Architecture:** Four Raptor processes share backend-owned domain services through HTTP; a separate Go Gateway owns its execution schema and simulated runtime. Durable outboxes and idempotent endpoints bridge services without cross-schema access. Browser polling exposes progress; the real cloud/runtime adapters follow in separate slices.

**Tech Stack:** Go >=1.25, PostgreSQL 17, pgx v5.11.0, official MCP Go SDK v1.8.0, standard-library HTTP, bcrypt, JSON Schema and existing plain JavaScript frontend assets. Pin resolved dependency versions and module checksums when implementation starts.

**Spec:** [POC service contracts and local specification](../specs/2026-10-04-platform-service-contracts.md).

## Global Constraints

- One public monorepo; no credentials, private context, legacy SQLite records or screenshot references committed.
- Raptor schema `raptor`; Gateway schema `gateway`; runtime roles `raptor_app` and `gateway_app` cannot access each other's schema.
- Agent request: one primary object and one environment; direct restart: explicit multiple targets, parallel execution.
- Basic Auth for service access; local browser username/password accounts; HTTPS remotely and loopback-only local HTTP.
- Approval wait 120 seconds; PR wait checkpoints/releases immediately; only one active agent execution.
- New skill release never silently upgrades existing requests. Changed skills preserve conversation/progress.
- Deregistration: healthy registered nodes remaining > ESS desired capacity / 2. Scale-in does not check deregistration; retain the accepted race.
- Zero cloud/model calls and zero paid resource creation in this slice. All adapter execution results visibly say simulated.
- Preserve existing Python prototype and runtime data. Ports 8870–8874 avoid existing 8765.
- No special replacement workflow, Infra API restart operation ID or new Infra API database.

## Review Focus

- Lost dispatch acknowledgement: retry must not create a second request/execution. Task 4 and Task 5 pin this.
- Review/approval arrives during checkpoint/cleanup: retain the signal without overlapping runtimes. Task 6 pins this.
- Restarting services with unresolved execution ownership: recovery must block rather than replay. Task 5 pins this.
- Browser switches requests before an old progress response arrives: do not display foreign progress. Task 11 pins this.
- Caller sends cross-environment references or secrets in tool output: reject the reference and persist only sanitized progress. Tasks 3, 7 and 11 pin this.

## Planned file structure

Paths below are repository-relative and name new files unless marked existing.

| Path | Responsibility |
| --- | --- |
| `components/raptor/go.mod`, `go.sum` | Raptor module and pinned dependencies |
| `components/raptor/cmd/{backend,frontend,open-api,admin}/main.go` | Four process entrypoints |
| `components/raptor/internal/domain/models.go` | Catalog/request/approval types; no Gateway storage |
| `components/raptor/internal/db/`, `migrations/` | Raptor-only PostgreSQL migrations and persistence |
| `components/raptor/internal/{auth,catalog,requests,approvals,skills,github}/` | Focused backend features |
| `components/raptor/internal/{backend,frontend,admin,openapi}/` | HTTP/MCP adapters |
| `components/raptor/operation-schemas/` | Version-controlled operation forms |
| `components/raptor/web/poc/` | New UI assets alongside untouched legacy UI |
| `components/raptor/internal/adapters/` | Gateway/Infra/repository clients and synthetic test adapters |
| `components/agent-gateway/go.mod`, `go.sum`, `cmd/gateway/main.go` | Separate Gateway process/module |
| `components/agent-gateway/internal/{execution,db,runtime,httpapi}/`, `migrations/` | Queue, history, lifecycle and Gateway-only storage |
| `docs/setup/go-platform-local.md` | Local preparation/start/acceptance guide |
| `docs/contracts/fixtures/` | Synthetic shared wire fixtures, not database code |

Tests live beside their owners. Each module has `internal/testutil/postgres.go` for isolated test schemas/roles and `internal/testutil/fixtures.go` for synthetic inputs. Fixture factories are helpers, not production seeded data. Use distinct test databases on the local cluster; do not reset a user's existing database.

## Task 1: Separate PostgreSQL ownership and process foundation

**Files:** Create both module files, both `internal/db/{connect,migrate}.go`, both `migrations/001_initial.sql`, both `internal/testutil/postgres.go`, Raptor `internal/domain/models.go`, Gateway `internal/execution/models.go`, both `internal/db/isolation_test.go`, and `docs/setup/go-platform-local.md`.

**Interfaces:** Each module independently exposes `db.Open(ctx context.Context, url string) (*pgxpool.Pool,error)` and `db.Migrate(ctx context.Context,pool *pgxpool.Pool) error`. Define Raptor types `Object`, `Environment`, `Request`, `Operation`, `RestartTarget`, `SkillsVersion`, `Approval`, `Event`. Define Gateway types `ExecutionInput`, `Execution`, `ProgressEvent`, `Signal`, `CheckpointRef`, `CleanupOutcome`.

- [ ] Write `TestSchemaIsolation` against real PostgreSQL: Raptor role can query Raptor tables and gets insufficient privilege on Gateway; reverse is also denied. Test migrations twice and unique object-code/environment-code constraints. Assert cross-schema failure: `if err == nil { t.Fatal("cross-schema access allowed") }`.
- [ ] Prepare local tooling only after plan approval: check Go >=1.25 and PostgreSQL 17 availability; if missing, install using the existing local package manager. Create a dedicated loopback PostgreSQL cluster under ignored `.raptor-local/go-platform/postgres` on port 55432, with hidden password input; document exact commands in the setup guide. Do not install Docker merely for this plan.
- [ ] Run `go test ./internal/db -run TestSchemaIsolation -v` in each module. Expect failure because schema/migrations are not implemented, not a connection failure. Test helper uses `TEST_DATABASE_URL` privately; do not print it.
- [ ] Implement service-owned tables/migrations and typed models from the spec, with separate owner/runtime roles and no cross-schema grants. Raptor tables: users/sessions, catalog, environment groups/environments, requests/targets, approvals, PR associations, skills changes and outbox. Gateway tables: executions/attempts, events, signals and runtime ownership. Run both isolation tests and `go test ./...`; expect PASS.
- [ ] Commit the foundation files and setup guide: `feat: add isolated Go platform persistence`.

## Task 2: Local user authentication and admin service

**Files:** Raptor `internal/auth/{service,http}.go`, `internal/auth/service_test.go`, `internal/admin/{server,templates}.go`, `internal/admin/server_test.go`, `cmd/admin/main.go`, `cmd/backend/main.go`, `internal/backend/server.go`.

**Interfaces:** `auth.Login(ctx,username,password string) (Session,error)`, `auth.Authenticate(ctx,token string) (User,error)`, `auth.Logout(ctx,token string) error`, `auth.RequireAdmin(user User) error`; service HTTP middleware `auth.BasicAuth(next http.Handler,credentials Credentials) http.Handler`.

- [ ] Write `TestSessionAndAdminPermissions`: bcrypt hash never equals password; valid login succeeds, invalid fails; expired/logged-out session denied; non-admin cannot administer users/envs; CSRF-free mutation denied. `if storedHash == password { t.Fatal("plaintext password persisted") }`.
- [ ] Run `go test ./internal/auth ./internal/admin -v`; expect missing implementation failures.
- [ ] Implement password/session/CSRF contracts and server-rendered admin login/user management. Bootstrap admin via explicit hidden-input command, no default credentials. Admin delegates storage to backend; Basic Auth uses configuration rather than browser account password.
- [ ] Run auth/admin tests and HTTP tests for no secret-bearing errors; expect PASS. Verify no listener defaults to a public address.
- [ ] Commit: `feat: add local login and Raptor admin accounts`.

## Task 3: Catalog and environment context

**Files:** Raptor `internal/catalog/{service,http}.go`, `internal/catalog/service_test.go`, `internal/admin/environments.go`, `internal/adapters/{infra,infra_fake}.go`, synthetic `docs/contracts/fixtures/deployments.json`.

**Interfaces:** `catalog.CreateObject(ctx,input CreateObjectInput) (domain.Object,error)`, `catalog.UpdateObject(ctx,id string,input UpdateObjectInput) (domain.Object,error)`, `catalog.GetEnvironment(ctx,code string) (domain.Environment,error)`; `InfraReader.ListDeployments(ctx,env domain.Environment,object domain.Object) ([]Deployment,error)`.

- [ ] Write `TestCatalogCreationDoesNotDeploy`, `TestEnvironmentRelationships` and `TestDiscoveryFailureIsNotEmpty`: creation calls no Infra mutation; codes unique/independent; same object can appear in multiple groups; same-env DB/proxy checks; lookup failure remains error. `if fake.MutationCalls != 0 { t.Fatal("catalog create deployed a resource") }`.
- [ ] Run `go test ./internal/catalog ./internal/admin ./internal/adapters -v`; expect behavior failures.
- [ ] Implement catalog/environment routes, admin forms and typed Infra read adapter with synthetic fixture implementation. Context returns live env config, not snapshots; exclude secret contents. Empty successful discovery returns an empty list. Reject cross-env target references.
- [ ] Run tests; update env config after request-context lookup and verify later lookup sees the edit. Expect PASS.
- [ ] Commit: `feat: add catalog objects and live environment context`.

## Task 4: Validated requests and durable handoff

**Files:** Raptor `operation-schemas/{database.deploy,db-proxy.deploy,db-proxy.replace-nodes}.json`, `internal/requests/{schema,service,http,outbox}.go`, `internal/requests/{schema,service,outbox}_test.go`, `internal/adapters/gateway.go`.

**Interfaces:** `requests.Validate(input CreateRequestInput,definitions SchemaRegistry) error`, `requests.Create(ctx,user User,key string,input CreateRequestInput) (domain.Request,error)`, `requests.DispatchPending(ctx,client GatewayClient) error`; `GatewayClient.PutRequest(ctx,requestID string) error`.

- [ ] Write tests for required form fields, wrong scalar types, unknown properties, multiple primary objects/environments, missing DB deployment, schema hash retention and retry after lost acknowledgement. `if first.ID != retry.ID { t.Fatal("duplicate request on retry") }`; same key/different body must return conflict.
- [ ] Run `go test ./internal/requests -v`; expect FAIL.
- [ ] Implement shared schema rendering metadata/backend validation, object-first create and transactional request/outbox persistence. Do not dispatch an invalid/uncommitted request. Persist direct target lists separately; preserve parallel semantics for Task 10.
- [ ] Run tests including fake Gateway accepting dispatch but response lost; expect one request/outbox identity and recoverable delivery, PASS.
- [ ] Commit: `feat: add structured requests and durable gateway handoff`.

## Task 5: Gateway queue, progress and simulated runtime

**Files:** Gateway `internal/execution/{store,queue,events,worker}.go`, `internal/execution/{queue,events}_test.go`, `internal/httpapi/{server,server_test}.go`, `internal/runtime/{adapter,simulated}.go`, `cmd/gateway/main.go`.

**Interfaces:** `Store.Receive(ctx,requestID string) (Execution,error)`, `Store.ClaimNext(ctx,owner string) (*Execution,error)`, `Store.AppendEvent(ctx,event ProgressEvent) error`, `Store.Events(ctx,requestID string,after int64) ([]ProgressEvent,error)`. Runtime methods/types exactly as spec; `RaptorClient.Context(ctx,requestID string) (ExecutionInput,error)`.

- [ ] Write `TestIdempotentReceive`, `TestSingleRuntimeOwner`, `TestProgressSurvivesRestart`, `TestUnresolvedOwnerBlocksDispatch`: duplicate PUT creates one execution; two workers start at most one runtime; progress persists; process restart does not replay unresolved execution. `if simulator.MaxActive != 1 { t.Fatal("overlapping agent execution") }`.
- [ ] Run `go test ./internal/execution ./internal/httpapi ./internal/runtime -v`; expect FAIL.
- [ ] Implement queue locking, event IDs/sequences, Basic Auth endpoints and simulated adapter with inert request-specific checkpoints. Store evidenceMode `simulated`. Reconcile lost ownership explicitly; do not implement timeout-based automatic rerun.
- [ ] Run tests with real PostgreSQL and concurrent fake workers; expect PASS, monotonic event order and no duplicate event writes.
- [ ] Commit: `feat: add durable Go gateway execution and progress`.

## Task 6: Pause, resume, interruption and cleanup

**Files:** Gateway `internal/execution/{signals,lifecycle}.go`, `internal/execution/lifecycle_test.go`, `internal/runtime/simulated_test.go`; Raptor `internal/requests/actions.go`, `actions_test.go`.

**Interfaces:** `Store.DeliverSignal(ctx,signal Signal) error`, `Worker.ApplySignal(ctx,execution Execution,signal Signal) error`, `Worker.Pause(ctx,execution Execution,reason string) error`; injected `Clock.Now() time.Time` and controllable fake timers.

- [ ] Write tests for approval at 119s retaining runtime, no decision at 120s checkpointing/releasing, PR pause immediate release, signal during cleanup retained, checkpoint/cleanup failure blocking slot, Block/Cancel history and no automatic rollback. `if simulator.MaxActive > 1 { t.Fatal("resume raced cleanup") }`.
- [ ] Run `go test ./internal/execution -run 'Test(Pause|Signal|Cancel|Block)' -v`; expect FAIL, using a fake clock rather than sleeping two minutes.
- [ ] Implement durable signals, lifecycle transitions, exact 120-second approval timer and explicit recovery outcomes. Save checkpoints before releasing ownership; queue continuation until prior runtime stopped. Refuse new operations for blocked/cancelled executions.
- [ ] Run lifecycle tests and restore a checkpoint into a fresh simulated runtime; conversation fixture and progress remain request-specific. Expect PASS; no claim of real Pi restoration.
- [ ] Commit: `feat: add request pause resume and cleanup ownership`.

## Task 7: Bound human approvals and Raptor MCP

**Files:** Raptor `internal/approvals/{service,http}.go`, `service_test.go`, `internal/openapi/{server,mcp}.go`, `mcp_test.go`, `cmd/open-api/main.go`.

**Interfaces:** `approvals.Request(ctx,input ApprovalInput) (domain.Approval,error)`, `approvals.Decide(ctx,user User,id string,input DecisionInput) (domain.Approval,error)`, `approvals.Check(ctx,input ApprovalCheckInput) (ApprovalCheckResult,error)`; MCP tool names/transport from spec, backend clients for delegation.

- [ ] Write tests: matching approval allowed; changed target/params/env denied; new action ID pending even with same parameters; unavailable verifier gives no permission; deny requires guidance/preset; creator self-approval permitted. `if changedParameters.Allowed { t.Fatal("approval widened") }`. Official SDK client must initialize/list/call tools; ensure no catalog mutation tool exists.
- [ ] Run `go test ./internal/approvals ./internal/openapi -v`; expect FAIL.
- [ ] Implement canonical parameter comparison, decision/signals outbox and verification endpoint. Expose `/mcp` using official SDK Streamable HTTP, Basic Auth and validated request-scoped inputs. No custom JSON imitation of MCP.
- [ ] Run SDK-client contract tests and verify a cross-env `deployments_list` tool request is rejected. Expect PASS.
- [ ] Commit: `feat: add bound approvals and Raptor MCP access`.

## Task 8: PR linkage and GitHub webhook continuation

**Files:** Raptor `internal/github/{association,webhook,delivery}.go`, `webhook_test.go`; synthetic `docs/contracts/fixtures/{review-changes,review-approved,pr-merged}.json`.

**Interfaces:** `github.AttachPR(ctx,requestID string,input PRInput) error`, `github.HandleDelivery(ctx,deliveryID,eventType string,body []byte) error`; delivery invokes the durable signal outbox from Task 4, not Gateway tables.

- [ ] Write signed fixture tests: changed/commented submitted review resumes; approving review waits; merge queues continuation; ordinary comment ignored; bad signature rejected; duplicated delivery one signal; event during cleanup preserved; cancelled request never resurrected. `if duplicate.SignalCount != 1 { t.Fatal("duplicate wake-up") }`.
- [ ] Run `go test ./internal/github -v`; expect FAIL.
- [ ] Implement canonical PR association, HMAC verification, delivery deduplication and events mapped only to associated requests. Record outcomes even for ignored late events; approve is not auto-merge or apply success.
- [ ] Run tests with fake Gateway unavailable during delivery and then recovering; expect durable queued signal, PASS.
- [ ] Commit: `feat: add GitHub review and merge continuation`.

## Task 9: Published skills selection and version switching

**Files:** Raptor `internal/skills/{releases,service,http}.go`, `service_test.go`; Gateway `internal/execution/{skills,retries}.go`, `skills_test.go`, `retries_test.go`; `agent-skills/README.md` documenting release convention, not a prescribed replacement skill.

**Interfaces:** `ReleaseSource.List(ctx) ([]domain.SkillsVersion,error)`, `skills.Resolve(ctx,tag string) (domain.SkillsVersion,error)`, `skills.Change(ctx,user User,requestID string,input SkillsChangeInput) error`; Gateway `Worker.ApplySkillsChange(ctx,execution Execution,signal Signal) error`, `Store.AllowApplyRetry(ctx,requestID,originalRunID string) (bool,error)`.

- [ ] Write tests: highest stable semver default; older selection accepted; mismatched SHA rejected; tag movement never changes stored SHA; interrupt restores conversation/progress with new skills; next-pause applies on approval/review only. Retry allowance persists across process/session restarts: first two allowed, third denied. `if thirdAllowed { t.Fatal("retry allowance reset") }`.
- [ ] Run skill/retry tests in their modules; expect FAIL.
- [ ] Implement repository-read adapter and synthetic releases, selection/applied version audit, skill-change signals and lifecycle integration. No registry, hot reload or accidental model/image upgrade. Track apply retries by original run ID.
- [ ] Run tests including failed interruption cleanup blocking replacement and completion before next pause not restarting request. Expect PASS.
- [ ] Commit: `feat: add request skills versions and retry bounds`.

## Task 10: Direct restart batch against the common status interface

**Files:** Raptor `internal/requests/{restart,restart_store}.go`, `restart_test.go`; `internal/adapters/infra.go` extend with mutation/status methods; synthetic fixture `docs/contracts/fixtures/restart-status.json`.

**Interfaces:** `InfraCommands.RestartDeployment(ctx,requestID string,target domain.RestartTarget) error`, `InfraCommands.GetDeploymentStatus(ctx,target domain.RestartTarget) (DeploymentStatus,error)`, `requests.RunRestartBatch(ctx,requestID string,client InfraCommands) error`.

- [ ] Write `TestRestartsStartInParallel`, `TestPartialFailureContinues`, `TestRetryOnlyFailedTargets`, `TestUnknownSubmissionNotRepeated`. Use barriers, not wall-time assumptions: both fake calls start before either finishes. `if fake.CallsForSuccessfulTarget != 1 { t.Fatal("successful target restarted again") }`.
- [ ] Run `go test ./internal/requests -run TestRestart -v`; expect FAIL.
- [ ] Implement explicit target validation, persisted per-item outcomes and independent status polling. Use common Deployment state, no Infra operation ID. Enforce proposed ten-minute observation deadline; an HTTP accepted restart is not success. Provider adapter remains synthetic.
- [ ] Run tests for equal workload names in different namespaces, conflicting UID, lookup error and timeout reporting; expect PASS and honest unknown outcomes.
- [ ] Commit: `feat: add parallel restart requests and rollout progress`.

## Task 11: Frontend forms and live request progress

**Files:** Raptor `cmd/frontend/main.go`, `internal/frontend/{server,proxy}.go`, `server_test.go`, `web/poc/{index.html,app.js,style.css}`, `tests/harness/go-platform-web.test.mjs`; extend admin templates for environment/user UI.

**Interfaces:** Browser client `api(path,{method,body})`, `renderOperationForm(schema)`, `loadRequest(requestId)`, `pollEvents(requestId,cursor)`, `renderProgress(events)`. Backend endpoints from spec; frontend proxy owns browser session/CSRF forwarding, not Basic Auth secrets in JavaScript.

- [ ] Write Node tests for required form validation, grouped env selector, saved history, two-second polling with fake timers, request-switch stale response ignored, sanitized expandable tool results and no false empty discovery on error. `assert.equal(view.requestId, currentRequestId)` after an old response completes.
- [ ] Run `node --test tests/harness/go-platform-web.test.mjs` from repo and Go frontend tests; expect FAIL.
- [ ] Implement object list/detail tabs, operation forms, direct restart selection, Request page, approval/denial controls and skills version strategy selector. Use old Raptor visual patterns without embedding private screenshots. Render dynamic text safely and label simulated progress. Preserve legacy web assets unchanged.
- [ ] Run Go/Node tests; inspect the running UI at 8870 with synthetic data after implementation. Confirm refresh restores history, closing page does not stop work, tool detail secrets excluded and admin routes remain permission-bound.
- [ ] Commit: `feat: add POC catalog and live request UI`.

## Task 12: Local acceptance, regression and handoff

**Files:** Raptor `internal/acceptance/platform_test.go`; Gateway `internal/acceptance/lifecycle_test.go`; `docs/setup/go-platform-local.md` update; `docs/setup/go-platform-local-acceptance.md`; root `README.md` update only after verified result.

**Interfaces:** Real HTTP processes with test PostgreSQL and synthetic runtime/provider/repository adapters; no external network dependency. Adapter injection must be explicit, never enabled as production success fallback.

- [ ] Write an acceptance scenario creating a DB object without deployment, validating/dispatching a request, viewing saved/live progress, pausing, changing skills and restoring the same request. Add PR-feedback/merge, denied approval and direct batch partial-success scenarios. Assert `evidenceMode == "simulated"` everywhere in the receipt.
- [ ] Run module acceptance tests before wiring the scenario; expect meaningful failure if any required boundary is missing.
- [ ] Complete fixture/process wiring and documentation. Add simple development start/stop instructions with explicit process ownership and credential references; no killing unrelated services or resetting legacy data.
- [ ] Run `go test -race ./...` and `go vet ./...` in each module, `node --test tests/harness/go-platform-web.test.mjs`, existing offline Python/Node regression commands from repo setup docs, and local acceptance. Record exact commands/results, source commit and simulated limits; do not claim cloud POC completion.
- [ ] Commit verified docs and code. Request whole-branch review using the execution method selected by the user, fix actionable findings, and present a reviewable PR. Do not merge it without current authorization.

## Plan self-review and scope coverage

Tasks 1–3 implement ownership, accounts and catalog/env context. Task 4 implements validated object-first requests. Tasks 5–6 implement durable queue/progress/session lifecycle. Task 7 implements approval/MCP contracts. Tasks 8–9 implement PR events, selected skills and persisted retry limits. Task 10 implements direct restart semantics; Task 11 exposes them in the requested UI; Task 12 verifies integration and regressions.

Real Pi/OSS checkpoint restoration, cloud Infra API and live acceptance are intentionally outside this slice, not silently covered by fake tests. Those plans follow after their provider/runtime contracts are verified. The local environment inspection did not find Go, Docker or psql on the shell PATH; tooling readiness must be established in Task 1 after plan review, rather than claimed ready now.

## Execution handoff

Planning only: no dependencies installed, cloud resources provisioned or product source changed by this document. Review the proposed contract details and this plan before implementation. The user earlier selected native execution; retain that preference unless they change it. Native implementation requires `superpowers:executing-plans` and a final independent branch review as described by that skill. Do not dispatch implementation agents while this plan remains unreviewed.
