# Raptor-resource Infra API refactor

Status: owner approved refactor direction and requested Superpowers workflow. Design details below are implementation decisions for review, not deployed facts.

## Goal and scope

Every agent-facing Infra call selects a Raptor resource using envCode and resource code. Agent discovers codes through Raptor MCP. Infra resolves provider targets internally and returns Raptor workload objects. Eliminate INFRA_SCOPE_FILE/scope.json from production startup and packaging. No AWS implementation, new service, controller, cache, cloud resource or permission grant in this source refactor. Existing Alibaba SDK adapters, credentials, guards and GitOps remain.

## Approaches

1. Hide provider IDs in Gateway while retaining static Infra scope: rejected because it leaves duplicated mappings and provider-shaped API authority.
2. Let Raptor pass cloud targets in every tool call: rejected because caller arguments remain provider authority and catalog/Infra can diverge.
3. Selected: authenticated catalog-only context read, provider discovery inside Infra, logical workload API. Raptor owns resource existence and environment configuration; verified tags and associations determine current cloud realization. Persisted request identity pins protect approved operations from silently moving targets.

## Catalog context and circular dependency

Add a service-authenticated Raptor backend GET /v1/infra/resource-context?envCode=...&kind=...&code=.... It reads Catalog.GetEnvironment and FindObject ONLY; NEVER Catalog.Deployments or Infra HTTP. Return a strict minimal projection: canonical code/kind, envCode, cloud, region, accountId, clusterId. Do not return arbitrary environment Config, credentials, caller-chosen endpoints, or cloud targets. Add logical dependent application codes from an explicit Raptor environment setting `proxyApplications` mapping canonical proxy code to a list containing exactly one application code for this POC. Validate these objects exist as applications. No cloud IDs enter that setting. Multiple dependent application codes are explicitly unsupported in this refactor and fail admission rather than expanding restart scope. Absent/ambiguous dependency fails replacement admission; never infer unrelated apps by name.

Reuse existing trusted Raptor service authentication and origin configuration; do not issue credentials or broaden audiences. Infra uses a fixed configured origin, bounded requests, no redirects, no external dynamic endpoints. Catalog context is re-read per operation, not cached. Normalize ACK identity from structured ackClusterId and Config clusterId; if both are present they must agree, otherwise use the single present value. Missing cluster identity fails application/metrics operations. Generic service discovery may read logical objects; request-scoped agent operations additionally require current token introspection and resource authorization before provider discovery.

## Provider resolution

Introduce ResourceResolver interface consuming ctx, kind/code/envCode and trusted catalog context. Existing typed Environment and ProxyMapping may remain INTERNAL request-local structs; no local file populates them in production.

Proxy: enumerate ESS groups with env and db-proxy-code tags, verify complete pagination/detail tags and exactly one matching group; target-db-code must resolve an existing Raptor database. Validate exactly one NLB attachment and matching port. Resolve its listener using NLB server-group/load-balancer relationships and bounded listener enumeration, checking returned identities and uniqueness. Reject missing, duplicate, unsupported or foreign relationships. Account identity must match catalog account before reads. Only Alibaba Singapore is supported initially; unknown provider/region errors explicitly.

Application: discover Deployments through existing raptor.appcode label in catalog cluster, preserve namespace/name/UID as internal exact identity. Read can return multiple workloads; restart requires an unambiguous workload key or exact request-bound workload. Never restart every matching deployment implicitly. Database: current tag-based RDS discovery, projected as databaseWorkloads.

Provider listener enumeration is a NEW read capability, not a new resource. Existing Infra IAM does not clearly grant ListListeners/GetServerGroupAttribute. Source tests may stub it, but deployment stays held pending exact IAM action/resource analysis and reviewed Terraform plan/comment/apply if a grant is needed. Do not borrow operator or Gateway permissions or add wildcard grants as a workaround.

## Agent-facing contracts

Keep existing catalog kind `db-proxy` and input field `proxyCode` to avoid needless code alias churn; response noun is `pgproxyWorkload` as owner requested. Tool names remain where semantics fit. Public inputs reject provider selectors even if they happen to match the backing resource.

- deployments_list(envCode, kind, code): resource-rooted list with typed workloads for application/database/proxy. Backend browser adapter consumes same resource projection.
- deployment_status_get(envCode, appCode, workloadKey optional): logical application workload status. Multiple matches require explicit returned workloadKey; key is opaque and resolved only under app/env, not a cluster override.
- proxy_observation_get(requestId, envCode, proxyCode, kind): kind fleet/backend/capacity retained for bounded observations; result is resource envelope {envCode,proxyCode,observedAt,pgproxyWorkload:{type:"ess",...selected observation}}.
- proxy_pgcat_metrics_get(requestId,envCode,proxyCode,series): existing finite series names; resource envelope {envCode,proxyCode,observedAt,pgproxyWorkload:{type:"ess",metrics:{series,rows}}}. Preserve raw sample/source timestamps and missing/duplicate detection; do not replace missing with zero.
- deployment_restart(requestId,envCode,appCode,workloadKey,actionId where existing runtime needs it): resolve and compare exact request pin before patch.
- db_proxy_scale(requestId,actionId,envCode,proxyCode,desiredCapacity): no groupId.
- db_proxy_node_protection_set(requestId,envCode,proxyCode,nodeIds,protected) and db_proxy_nodes_deregister(requestId,envCode,proxyCode,nodeIds): resolve node IDs only within freshly discovered workload membership. Node references may retain current stable instance ID representation INSIDE resource namespace; they cannot select an arbitrary cloud resource.
- cloud_identity_get: remove from agent tool catalog and qualification check list. Retain account verification internally. If a legacy service-only diagnostic is retained temporarily, it must not become normal agent execution.

MCP and HTTP share logical contracts. Do not retain an active HTTP bypass accepting arbitrary old provider selectors. Previously persisted request/approval/result records stay readable; old active argument contracts fail closed with an explicit migration error. No rewriting immutable skills-v0.4.0 or historical evidence.

## Authorization, approvals and pins

Raptor MCP exposes primary resource and authorized dependent resource codes for the request. Scoped `deployments_list` currently absent: add request-bound resource discovery/context rather than widening service MCP wholesale. Primary/dependent codes are authorized against catalog context, not free-form model claims.

At access issuance, Raptor obtains resolved workload identities from Infra via trusted service resource reads. Infra catalog lookup above cannot recurse. Store exact identities in existing attempt access replacement_scope server-side for continuity; this is an execution pin, not a second manually maintained resource source. On introspection resolve again and reject any changed group/backend/app UID/target DB. Remove production private replacement-scope file requirement in backend startup.

New-format attempt proofs require ResolutionDigest: lowercase SHA-256 of a deterministic canonical JSON tuple containing envCode/accountId/region/clusterId, proxyCode/groupId/serverGroupId/listenerId/port, targetDbCode and actual RDS resource identity, application envCode/appCode/clusterId/namespace/name/UID. Exclude capacity, node membership, timestamps and resourceVersion because legitimate runtime changes must not invalidate resource identity. Every new-format authorization compares this digest after fresh resolution. Historical records lacking it remain readable but cannot authorize new-format mutations. Replacement admission requires exactly ONE deployment identity for the dependent application. More than one fails admission; ordinary read discovery may list them. Across pause/resume, expiry and new attempt IDs under the SAME request, inherit the first new-format ResolutionDigest and persisted baseline, never silently refresh to a different realization. A changed realization requires a new reviewed request. Historical requests without a new-format digest cannot be upgraded into mutation authorization implicitly. Authorize logical selectors before any provider-backed revalidation during introspection. Initial resolution can return a service-only proof DTO; do not put private auth details or proof construction into model arguments.

Agent token binding and private introspection can retain internal provider pins. Gateway must not expose them as required model parameters. Infra re-introspects each operation, resolves resource, compares current realization with attempt pins, then constructs existing internal command structs for unchanged policy/runtime. Both boundary authorization implementations change consistently.

Approval_request becomes resource-based: interface `db-proxy.scale-in` and target {proxyCode}; parameters desiredCapacity. Server privately binds the resolved execution identity when recording/claiming approval; existing historical ess.scale-in approvals remain readable but cannot authorize new resource-contract attempts. Qualification resume remains harmless and resource-bound. Prevent same proxy code retargeting after approval using pinned identity/version in private claim and replay checks. Existing minimum change approval remains required; no new minimum mutation API in this refactor. Minimum stays Terraform-owned.

## Invariants

Preserve desired>=minimum; approval for minimum changes and desired reductions; minimum healthy serving capacity; expand before withdrawing BOTH old nodes; restart only authorized related app workloads; fresh complete per-pool/user metrics; combined retiring active+idle+waiting<=1; user accepts one lost connection only; exact action claim/no replay, observation freshness, pause/resume continuity, no automatic uncertain reversal. SQL failure remains FAIL. Replacement remains disabled throughout refactor/qualification. No native completion admission shortcuts.

## Consumer migration and release

Update Raptor Infra HTTP adapters, scoped MCP and authorization, Infra APIs/runtime/startup/metrics, Gateway private MCP route/envelope adapter and harness producer contracts, probes/current docs/tests. Existing internal provider-shaped guard snapshots may stay; translate at server boundary. Do not rewrite independent traffic collector into a new architecture. Update only active documents; archived/historical records remain untouched.

Source-only tasks proceed serially with TDD/task review, then ONE whole-branch review and focused fixes. No deployment while interfaces are mixed. PR305 old pins are superseded/held; prepare fresh receipts after reviewed merge. Real read-only qualification must demonstrate catalog codes -> Infra workloads -> actual metrics, denied foreign/changed identities, persisted baseline, harmless approval/pause/resume/fresh reads and final cleanup before claiming its PASS. Full replacement remains a separate NOT PASS gate.

## Verification

Contract tests reject cloud IDs/URLs/queries, unknown/duplicate fields, foreign codes, unsupported provider, token revocation, duplicate discoveries, missing tags, changed mappings/UIDs, dependency ambiguity, redirect and oversized responses. Assert zero provider calls before resource authorization. Mutation regression tests prove existing guard behavior and approval identity binding. Test HTTP/MCP parity and exact Gateway producer/consumer envelopes. Run appropriate Go tests+vet, relevant Node harness and Python fixtures once after final source changes; preserve existing evidence timing rules. Update package/startup tests to prove no scope.json required or bundled. No live paid run to discover missing source integration.

## Final-review F1 equivalence — 2026-10-31

For authorization originating inside Infra, fresh introspection is realized as a private two-phase exchange using the existing inspector/service credentials: Raptor checks current token/request/lifecycle and exact logical operation before provider work; Infra resolves the complete proof locally in the current invocation; Raptor validates that proof against current catalog relationships and immutable request pins, and rechecks eligibility for the same operation. A prepared digest binds the exact operation and eligibility snapshot; it is not bearer authority, a cached proof or a permission to skip validation. Every replacement/qualification read (including ordinary deployments/status), command and MCP discovery follows this exchange. Application-question operations still get current lifecycle/selector checks.

Private approval Check/Claim similarly precheck the exact approved action and request eligibility before Infra's local fresh proof. Final validation binds the same action/parameters and prepared snapshot, uses the existing verifier and locked rechecks, and Claim retains atomic one-time consumption. Proof is carried only on dedicated private service-authenticated endpoints, with strict JSON/schema validation; no public/model proof field or provider selector is added. Provider changes cannot be atomically locked with the database; unchanged runtime fresh snapshots, UID/resourceVersion, metrics deadlines and replay guards remain mandatory.

Raptor-origin issuance, ordinary introspection and approvals without this private exchange retain fresh Infra fetches. This implements the fresh-resolution outcome in the paragraphs above without occupying another invocation of the same bounded Infra function. No audience-only bypass, semaphore, increased capacity, cross-call cache, new credential, cloud resource, IAM grant, service/controller or store is introduced. Unsupported protocol peers fail closed. Verification must saturate a two-slot real private HTTP graph for concurrent valid reads and approval-bearing commands, plus drift, foreign selector, revocation/expiry, exact operation/action and replay negatives.
