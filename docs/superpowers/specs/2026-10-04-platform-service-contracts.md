# POC service contracts and local Go platform specification

Date: 2026-10-04. Status: proposed concrete contracts for review. The product direction was reviewed in the current conversation; endpoint names, technical defaults and slice boundaries below are implementation proposals, not user quotations or deployed capabilities.

## Purpose, provenance and delivery slices

The accepted platform direction is an agent composing GitOps changes and individual Infra API commands for PostgreSQL RDS deployment, PgCat deployment on ECS/ESS and proxy replacement. Raptor manages objects and human requests; Gateway manages execution; skills guide operational strategy. No platform component contains a task-specific replacement workflow.

Detailed provenance stays in the private project workspace: `working/design/2026-10-04-poc-brainstorming.md`, Decisions 1–76, plus the user-reviewed platform and Infra API drafts. Those files are outside this public repository; do not publish private UI reference images or historical employer data. This specification restates the relevant requirements so implementation does not depend on private files.

Build in four separable slices:

1. Local Go platform: catalog, forms, requests, approvals, progress, MCP, GitHub event handling and Gateway lifecycle against explicitly simulated adapters. This document specifies that slice.
2. Real runtime: Aliyun sandbox/Pi, encrypted request checkpoints, operational tools, skills and read-only Aliyun CLI. Preserve existing authentication-checkpoint evidence and accepted refresh-token crash limitation.
3. Infra API: real provider discovery/restart/scaling/protection/deregistration adapters, Prometheus gates and Serverless Devs/FC/API Gateway deployment.
4. Cloud acceptance: Terraform/GitHub Actions foundation, ACK/Argo CD, test client, three live agent cases and measured outcomes.

The local slice neither provisions cloud resources nor claims real agent/cloud acceptance. Subsequent slices need their own reviewed specifications/plans, especially provider behavior and account pricing. Aliyun Singapore and cumulative Aliyun cost below RMB 1,000 remain whole-project constraints.

## Service layout and storage ownership

Use one Go module for Raptor's four processes under `components/raptor/`: `cmd/backend`, `cmd/open-api`, `cmd/frontend`, `cmd/admin`. Use another module under `components/agent-gateway/`. Preserve existing Python files and their SQLite database; there is no automatic data migration or deletion in this slice.

Raptor backend exclusively accesses schema `raptor`; the other three Raptor processes call backend HTTP. Admin serves its own HTML, forwarding user/environment administration to backend. Frontend serves HTML/JavaScript and proxies authenticated browser requests. Open API exposes agent MCP and service HTTP, delegating catalog/approval handling to backend.

Gateway exclusively accesses schema `gateway`. PostgreSQL is local and shared physically, with distinct migration owners and runtime roles `raptor_app` and `gateway_app`. Revoke public schema privileges and prevent cross-schema reads/writes. Migrations run with service-specific owner credentials; never ship those to a sandbox. No shared database storage package or cross-schema foreign keys.

Proposed local ports: frontend 8870, backend 8871, open-api 8872, admin 8873, Gateway 8874. Bind to `127.0.0.1` by default; do not stop the prior 8765 prototype. Use configurable PostgreSQL URLs, not embedded passwords. Go minimum 1.25; pgx v5.11.0; official MCP Go SDK v1.8.0. Pin dependencies in module lock files. PostgreSQL major 17 is a local development target, not the test RDS engine selection.

## Identity, catalog and environment contracts

Use opaque UUID identifiers. Automatically generate object codes, unique per kind (`application`, `database`, `db-proxy`), independent of related objects. Environment codes are globally unique and administered explicitly; stage names can repeat in different groups.

Catalog endpoints on backend `/api/v1`:

| Method/path | Input/result |
| --- | --- |
| `GET/POST /objects` | List or create `{kind,name,description}`; result `{id,kind,code,name,description}` |
| `GET/PATCH /objects/{id}` | Read/edit name and description; code/kind immutable |
| `GET/POST /environment-groups` | `{code,name}` |
| `GET/POST /environments` | `{code,groupCode,stage,config}` |
| `GET/PATCH /environments/{code}` | Current configuration, no request snapshots |
| `GET /objects/{id}/deployments?env=...` | Forward to the appropriate Infra API discovery adapter |

Environment configuration includes cloud (`aliyun`), account ID, region, ACK cluster ID, Terraform repository/path, Kubernetes repository/path and Infra API/Prometheus endpoint references. Secret references are allowed; secret contents are not returned in agent context. One ACK cluster per environment. Logical objects can deploy across groups. Deployment associations come from Infra API discovery, not automatic deployment on catalog creation.

Applications can use multiple DB deployments in the same environment, directly or through optional proxies. Each proxy deployment references one same-environment DB deployment; DB deployments may have multiple proxies. Provider discovery labels/tags: app `raptor.appcode`; database `env`,`db-code`; proxy ESS group `env`,`db-proxy-code`,`target-db-code`. Application dependency discovery beyond these identity tags belongs to a later integration contract; do not invent observed relationships in the local UI.

## Browser and service authentication

Browser users use local username/password accounts, password hashes and server-side sessions. Proposed password hashing: bcrypt, cost 12; sessions use random opaque tokens stored as hashes, eight-hour expiry, HttpOnly/SameSite=Lax cookies and CSRF tokens for mutations. Set Secure cookies under HTTPS. Administrator bootstrap is an explicit local command taking hidden input; no checked-in default password. Regular users can edit catalog objects/create requests and approve their own operations. Admin users additionally manage users/environments. No user hierarchy beyond those two roles is included.

Backend/Open API/Gateway service calls use Basic Auth. Remote transport requires HTTPS; loopback development can use HTTP. Service secrets are environment/file configuration excluded from Git, separate from browser passwords. Basic Auth does not provide per-request credential isolation. Validate supplied request/environment/object references in handlers. GitHub webhook ingress uses its signature mechanism, not Basic Auth.

## Request schema and operation forms

An agent request contains `{type:"agent",object:{kind,code},envCode,operations:[{name,parameters}],skills:{tag,commitSha}}`. Exactly one primary object and environment. Each operation must be valid for that object kind. Related objects appear as references in parameters/context; agents cannot create/edit catalog objects. Raptor persists the request before dispatch.

A direct restart request contains `{type:"direct",operation:"application.restart",targets:[{appCode,envCode,clusterId,namespace,name,uid}]}`. Different selected applications can occur in one batch; do not impose the agent's one-object rule on direct batches. Validate each target against its environment/application, deduplicate exact targets and retain per-target results. Never expand a batch into all workloads implicitly.

Version-controlled JSON Schema definitions live under `components/raptor/operation-schemas/`. Support objects, required properties, scalar types, enums, minimum values and defaults; frontend and backend use the same definitions. Reject unknown fields. Keep a definition version/hash on a submitted request so later form edits do not reinterpret its validated input; this is not an environment snapshot.

Proposed initial form names/fields:

| Operation | Object | Required parameters |
| --- | --- | --- |
| `database.deploy` | database | `engineVersion` string, `instanceClass` string, `storageGiB` integer > 0; engine fixed to PostgreSQL |
| `db-proxy.deploy` | db-proxy | `targetDbCode` string, `desiredCapacity` integer > 0, `instanceClass` string, `pgcatVersion` string |
| `db-proxy.replace-nodes` | db-proxy | No forced replacement sequence; parameters object initially empty |

These fields are proposed local forms, not verified cloud SKU/version choices. Do not pre-populate claimed valid Aliyun SKUs. Proxy deployment requires discovery confirming that the selected DB is deployed in the same environment. Local fixtures label their data synthetic.

## Raptor request and Gateway HTTP contracts

All JSON errors use `{error:{code,message,retryable}}`; never return raw credentials, SQL errors or provider log dumps. Use HTTP 400 invalid input, 401 unauthenticated, 403 unauthorized, 404 missing, 409 state/idempotency conflict, 503 unavailable dependency. Persisted events/results identify simulation versus live evidence.

| Endpoint | Contract |
| --- | --- |
| Backend `POST /api/v1/requests` | `Idempotency-Key` header and request body; identical retry returns same request, changed body with same key returns 409 |
| Backend `GET /api/v1/requests/{id}` | Request definition plus execution status fetched from Gateway; Gateway unavailable yields unavailable execution state, not invented success |
| Backend `GET /api/v1/requests/{id}/events?after=N` | Sanitized durable timeline, cursor and per-target direct results |
| Backend `POST /api/v1/requests/{id}/actions` | `{action:"block"|"cancel"|"continue",instructions?}`; continue requires instructions |
| Gateway `PUT /v1/requests/{id}` | `{requestId,type:"agent"}`; idempotent dispatch; fetch execution input through Raptor HTTP |
| Gateway `GET /v1/requests/{id}` | Runtime state, attempt ID, checkpoint/cleanup outcomes, selected/applied skills versions |
| Gateway `GET /v1/requests/{id}/events?after=N` | Ordered persisted events after cursor |
| Gateway `POST /v1/requests/{id}/signals` | `{signalId,kind,payload}`; unique signalId prevents duplicate continuation |
| Open API `GET /v1/requests/{id}/context` | Request object/operations/parameters, live env config, selected skills; no administration secrets |

Raptor records a durable dispatch outbox with its request transaction; a background dispatcher retries the same PUT. Gateway persists a received request before acknowledgement. Signals similarly use a Raptor outbox for review/approval/control events; network timeout cannot silently lose a wake-up. No message broker is needed.

Gateway proposal: states `queued`, `running`, `waiting_approval`, `waiting_review`, `blocked`, `interrupted`, `completed`, `failed`, `cancelled`. Persist execution attempts separately. One active runtime slot, enforced by PostgreSQL ownership plus a worker lock; never expire unknown ownership into a new run. Restart with an unresolved running attempt marks recovery-needed; local slice requires explicit reconciliation rather than replay. FIFO for new/ready resumptions by queue timestamp; waiting requests without runtimes do not hold the slot.

Runtime interface: `Start(ctx, ExecutionInput) (RuntimeHandle,error)`, `Checkpoint(ctx, RuntimeHandle) (CheckpointRef,error)`, `Stop(ctx, RuntimeHandle) (CleanupOutcome,error)`, `Restore(ctx, ExecutionInput,CheckpointRef) (RuntimeHandle,error)`. Simulated adapter stores request-specific files under ignored `.raptor-local/go-platform/` and cannot call cloud/model endpoints. Checkpoint references are opaque, not browser-downloadable archives. The real adapter is deferred.

## Progress, approval and MCP

Progress event `{eventId,requestId,attemptId,sequence,occurredAt,kind,summary,details,evidenceMode}`. Gateway assigns monotonic per-request sequence on persistence; use a unique source eventId for duplicate ingestion. Kinds cover status, tool start/result, approval wait, PR, checkpoint and cleanup. Sanitize before storage. Allowlisted structured summaries plus bounded text (16 KiB/event, 4 KiB/tool excerpt) exclude authorization headers, auth files, connection strings with credentials, callback URLs and known secret values. Do not claim redaction of every possible arbitrary secret. No hidden model reasoning.

Browser polls every two seconds while request page is open, reloads from last cursor and applies events only to the still-selected request. It continues showing saved history when Gateway is unavailable. Close the page without stopping execution. Polling is a proposed simple live-update mechanism, not an event-stream dependency.

Approval record `{approvalId,requestId,actionId,interface,envCode,target,parameters,state,guidance}`. `actionId` identifies one logical approval-required action, not an Infra API restart operation ID. Canonical JSON normalizes parameter object key order without coercing numbers/strings. Same actionId/matching body retrieves the existing decision; another actionId needs new approval even if body matches.

Open API `POST /v1/requests/{id}/approvals` requests approval; backend `POST /api/v1/requests/{id}/approvals/{approvalId}/decision` takes `{decision:"approve"|"deny",next:"block"|"cancel"|"continue",guidance?}`; next/guidance apply on denial, with custom continue requiring text. Verification endpoint `POST /v1/approval-check` takes exact request/action/interface/env/target/parameters and returns `{allowed,reason}`; unavailable authority means no authorization. Real scale-in execution/retry reconciliation is deferred to the provider contract; this endpoint alone does not prove exactly-once mutation.

Approval within two minutes resumes the current runtime. Otherwise checkpoint, stop and release; later decision queues restore. PR waits release immediately. Checkpoint/cleanup failure blocks new dispatch. Block stops new actions, checkpoints/releases after the runtime has stopped safely (proposed implementation choice); Cancel retains history and has no automatic rollback. In-flight external actions are not assumed cancelled.

Open API `/mcp` uses the official Go MCP SDK's Streamable HTTP transport, behind Basic Auth. Tools: `request_get`, `environment_get`, `object_get`, `deployments_list`, `approval_request`, `approval_get`, `pull_request_attach`, `request_pause`. They delegate to the same backend contracts. No catalog write or user/environment administration tools. `request_pause` carries `reason:"approval"|"review"|"blocked"` and related reference; it signals Gateway, not arbitrary shell execution. Validate tool arguments and one-environment request scope server-side.

## PR events, skills and direct restart contracts

Attach PR via Open API `POST /v1/requests/{id}/pull-requests` with `{repository,number,url,headSha}`. Validate canonical URL/repository/number agreement; many follow-up PRs can belong to a request. Github ingress `POST /webhooks/github` verifies HMAC signature, persists delivery ID and queues processing before 202. Duplicate deliveries do nothing. Review feedback may contain untrusted instructions; it cannot remove API gates.

Submitted changes-requested or non-approving reviews with feedback resume the request; approval keeps waiting; merge resumes for agent inspection of apply results. Ordinary comments/mentions do not wake it. Late events cannot resurrect cancelled/completed requests; preserve them in history. Queue events arriving during checkpointing until ownership is safely released. A merged PR is not a successful apply.

Agent GitHub tools will read logs, rerun failed jobs and monitor attempts: at most two automatic retries for temporary apply failures per original apply, persisted across sessions. Configuration errors use a follow-up PR; uncertain outcomes need inspection before retry. The local slice tests the persisted allowance, not real GitHub mutation permissions.

Skills endpoint backend `GET /api/v1/skills/releases` lists `skills-vMAJOR.MINOR.PATCH` tags and resolved commit SHAs; proposed latest means highest non-prerelease semantic version. Use a repository-read adapter; no fake published releases outside labelled fixtures. Store selected tag/SHA; new request defaults latest, allowing older releases. Entire `agent-skills/` folder is versioned together.

Backend `POST /api/v1/requests/{id}/skills` takes `{tag,commitSha,strategy:"interrupt"|"next-pause"}`. Reject mismatched/unpublished releases. Record actor/old/new versions. Track selected versus applied version. Interrupt retains conversation/progress and reconciles interrupted work before restoring with new skills; next-pause applies at approval or PR review pauses. Completing a request without such a pause does not restart it solely to apply pending skills.

Direct restart uses an Infra adapter: `ListDeployments(ctx,env,object)`, `RestartDeployment(ctx,requestId,target)`, `GetDeploymentStatus(ctx,target)`. Calls for different targets start in parallel. A failed item does not cancel others. Status polls show readiness and pod state. There is no persisted Infra API restart operation ID/database. Unknown restart submission is recorded unknown and inspected, not automatically repeated. Proposed rollout deadline ten minutes; timeout remains a truthful incomplete/failed-to-observe outcome, not proof that Kubernetes rolled back. Explicit retry applies only to confirmed failed targets.

## Infra API boundary retained for later implementation

HTTP catalog: app/DB/proxy discovery; Deployment status/restart; DB-proxy scaling; node protection enable/disable; node traffic deregistration. Proposed endpoint paths are deferred to its provider specification. Agent checks scaling results using read-only Aliyun CLI and queries runtime metrics through a Prometheus tool. Infra API queries Prometheus HTTP API itself for required checks.

- Proxy scaling requires exactly one matching ESS group.
- Scale-in requires Raptor approval and zero connected clients, including idle client sessions, on every unprotected node. Required metrics missing/stale/unavailable cause refusal.
- Scale-in deliberately does not verify deregistration. The user accepted the risk of new clients arriving between the metrics check and termination.
- Deregistration needs no human approval; healthy registered nodes remaining must be strictly greater than half ESS desired capacity. For desired four, at least three remain; desired three, at least two.
- Protection enable/disable needs no human approval; validate node membership/environment.

No replacement workflow, direct node-delete command, proxy scaling-status API or Infra API restart journal is added. Metrics mapping/freshness, load balancer semantics, provider concurrency and desired-state reconciliation require verification before cloud implementation. The target remains zero failed application operations measured by the client, not guaranteed by the accepted simplified gate.

## Local acceptance and limitations

Tests use a real local PostgreSQL cluster with both restricted service roles, HTTP/MCP clients and simulated runtime/provider fixtures. Prove durable request submission/progress, independent service restart, one active agent execution, approval target/parameter matching, pause races, skills changes, webhook idempotency and parallel restart results. Validate login/logout, admin restrictions and no cross-schema access. Two fake users and fixtures are generated only in test setup.

Local acceptance includes creating a new DB object without deployment, submitting a validated agent request, seeing simulated progress without keeping the browser open, simulated pause/resume with a selected skills change, and direct restart batch partial outcomes. The UI labels all simulated runs. Do not promote this result to a real Pi/sandbox or infrastructure acceptance receipt.

## Primary dependency references

Use the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) and its [v1.8.0 release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0), not a custom JSON endpoint labelled MCP. Use [pgx v5.11.0](https://github.com/jackc/pgx/releases/tag/v5.11.0); its release notes retain Go 1.25 as the minimum. These primary references were checked during planning, not installed.
