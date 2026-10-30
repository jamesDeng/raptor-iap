# Raptor Resource Infra API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace provider-selected Infra API contracts and scope files with Raptor resource selectors, internal resolution, typed workload results and preserved execution safety.

**Architecture:** Raptor provides authenticated catalog-only context; Infra discovers provider realization and returns logical workloads. Service-only immutable proofs connect Raptor issuance/introspection and approvals without a circular discovery/authentication call. Gateway translates workload DTOs to existing internal observations.

**Tech Stack:** Existing Go services, MCP Go SDK, Alibaba Go SDK, PostgreSQL catalog, Node MCP client/harness, Terraform-owned IAM.

**Spec:** docs/superpowers/specs/2026-10-30-raptor-resource-api-design.md

## Global Constraints

- Existing checkout only; no clone/worktree, preserve unrelated untracked files. One source/index editor at a time; no source edits on main.
- Inputs select envCode + Raptor resource code; provider IDs are not public selectors. Internal provider adapters/immutable pins may contain IDs.
- Eliminate production INFRA_SCOPE_FILE/scope.json and private provider replacement-scope file requirements; no substitute static provider map.
- Exactly ONE dependent application code for this POC in proxyApplications; missing/ambiguous dependency fails closed.
- Replacement admission requires exactly one dependent Deployment; new attempts under the same request inherit its first ResolutionDigest and baseline. Changed realization requires a new reviewed request.
- New attempts require ResolutionDigest over immutable resolved identities. No membership/capacity/timestamp in identity digest.
- Preserve existing approvals, replay controls, desired>=minimum, healthy serving floor, withdrawal/restart ordering, fresh complete combined retiring active+idle+waiting<=1 and SQL failure semantics.
- Replacement disabled; skills-v0.4.0 immutable; no native collector/controller expansion, AWS implementation, new resource or IAM grant.
- Source-only until coordinated tests/review; Terraform handles any newly needed least-privilege IAM via reviewed plan/comment/apply. No direct permission grants or deployment of mixed contracts.
- CNY1800 net ceiling; no second billing check Oct30. 21:30 mutation cutoff, 22:30 work stop Shanghai; existing 23:59 shutdown unchanged.

## Review Focus

- Recursive discovery/introspection: context route must never call Infra; service proof resolution must never introspect an agent token.
- Same-code retargeting: resolved listener/RDS/app UID drift rejects existing attempt and approval.
- Ambiguous tags/workloads or conflicting ACK fields: fail before mutations, never choose first result.
- HTTP bypass / model-injected cloud IDs: same strict resource selectors on HTTP and MCP; no legacy active route bypass.
- Half migrated consumers or immutable old skills: explicit migration failure; historical results readable, no fabricated acceptance.

---

### Task 1: Catalog-only resource context

**Files:** Create components/raptor/internal/catalog/resource_context.go and resource_context_test.go; create components/raptor/internal/backend/infra_context.go and infra_context_test.go; modify internal/domain/models.go and internal/backend/service.go only for types/registration.

**Interfaces:** Produce domain.InfraResourceContext {EnvCode,Kind,Code,Cloud,Region,AccountID,ClusterID,RelatedResources}; RelatedResources []domain.ObjectRef. Produce catalog.Service.InfraResourceContext(ctx context.Context, envCode,kind,code string) (domain.InfraResourceContext,error). Register GET /v1/infra/resource-context in existing service Basic-auth boundary with exact envCode/kind/code selectors; no redirects or deployment lookup involved. Code is canonicalized by FindObject. RelatedResources has exactly one application ref for a configured proxy; application/database resources have empty array. Missing proxy dependency may be represented as no related resource for ordinary reads, but replacement admission must reject it; malformed/multiple mappings fail context resolution. ACK structured/config fields must agree if both present.

- [ ] Write failing tests: TestInfraResourceContextCatalogOnly, CanonicalResource, ForeignOrMissingResource, InvalidProxyApplications, ClusterConflict, StrictServiceRoute. Inject fake minimal catalog-read seam if needed for unit tests; it must exercise same production projection and registration. Count zero deployment/Infra calls.
- [ ] Run targeted go tests; record actual expected failure before production changes.
- [ ] Implement strict minimal projection, single logical application lookup, field normalization and service-authenticated route. Never export arbitrary Config or credentials. No cloud IDs in dependency settings; validate via existing catalog APIs on read.
- [ ] Run focused packages and go vet; commit task-owned paths only and report test evidence and interfaces.

### Task 2: Catalog client and internal provider resolver

**Files:** Create components/infra-api/internal/resources/{catalog.go,resolver.go,proof.go,*_test.go}; modify internal/domain/models.go; internal/provider/{proxy.go,proxy_cloud.go,rds.go,pagination.go} plus new resource_resolution.go/tests.

**Interfaces:** Consume Task1 context JSON. Produce resources.Catalog interface { Context(context.Context,string,string,string)(domain.ResourceContext,error) }; resources.Resolver interface { Resolve(context.Context,string,string,string)(domain.ResolvedResource,error) }. ResolvedResource contains trusted Environment and exact internal target(s), typed workload representation and ResolutionDigest. HTTP catalog constructor accepts fixed origin/service credentials/client; rejects redirects, oversized/non-JSON/foreign envelopes, inconsistent canonical identities. Provider resolution implements resource lookup using current typed SDKs, tags and associations, never a file mapping. Service-only proof DTO fields defined in proof.go; digest includes exact tuple from spec.

- [ ] Write failing tests for catalog transport bounds, unauthorized logical resource before provider calls, unique complete ESS discovery, foreign account/tags, duplicate NLB/listener association, UID ambiguity, conflicting ACK, unsupported cloud/region and stable digest across fleet/capacity changes versus changed listener/RDS/app UID.
- [ ] Run targeted tests RED; inspect installed official SDK method signatures locally before wiring new NLB calls. No guessed APIs or new dependencies.
- [ ] Implement resolver and proof; no active startup wiring yet. New NLB enumeration stays typed/bounded; do not change mutation validation algorithms. Resource adapters expose workload type ess/kubernetes/rds as appropriate.
- [ ] Run provider/resources/domain tests+vet; commit and report precise new IAM read actions/resources for release gate. No Terraform apply or manual grants.

### Task 3: Infra resource HTTP/MCP API boundary

**Files:** Create components/infra-api/internal/api/resource_server.go, resource_operations.go, resource_mcp.go, resource_commands.go and matching tests; modify shared internal/api helpers and domain structs only when necessary. No cmd/server production wiring yet; no Raptor/Gateway edits in this task.

**Interfaces:** Consume Task2 resources.Service.Resolve and ResolveProof, domain.ResolvedResource, resources.Proof and existing internal Commander/Reader. Produce api.NewResourceServer(reader Reader,resolver ResourceResolver,username,password,authHeader string,commander Commander,authorizer Authorizer,check AgentCheck) (http.Handler,error), where ResourceResolver has Resolve(context.Context,string,string,string)(domain.ResolvedResource,error) and ResolveProof(context.Context,string,string)(resources.Proof,error). New handler exposes logical resource contracts from spec; old active handler replaced only in Task4, never mixed deployed.

Service-only GET /v1/resource-resolution-proof uses envCode/proxyCode and returns Task2 Proof. It is never an agent MCP tool. Read endpoints: /v1/deployments (envCode/kind/code) returns typed resource workload envelope; /v1/deployment-status (envCode/appCode/optional workloadKey) resolves within app, rejects ambiguous unspecified selection. Proxy observation/metrics stay MCP-only; required args requestId/envCode/proxyCode/kind or series. Return pgproxyWorkload type ess with selected observation/metrics nested, retaining exact provider evidence fields privately/nested. No cloud_identity_get tool or cloud identity HTTP operation in new handler.

Commands use existing paths/tool names except logical scale approval interface handled Task4: scale(requestId,actionId,envCode,proxyCode,desiredCapacity); protection(requestId,envCode,proxyCode,nodeIds,protected); deregister(requestId,envCode,proxyCode,nodeIds); restart(requestId,envCode,appCode,workloadKey). Public structs must not reflect internal GroupID/serverGroupID/cluster/namespace/name/UID as schema inputs. Resolve node refs only within current proxy membership through existing snapshot/command validation. Resolve app key only within same app/env and compare private bound identity. Translate to existing internal command structs/runtime without changing algorithms. Qualification cannot mutate.

- [ ] Write RED contract tests for logical-only HTTP/MCP calls, strict unknown/duplicate/nonstring selectors, cloud ID/URL/query injection denial, unsupported/foreign resource before any provider read, service-only proof agent denial, app workload ambiguity, resolved exact internal command target and existing qualification guard. Use synthetic context/resolver/backend seams, no cloud credentials.
- [ ] Authenticate service principal or opaque agent token, authorize logical selectors before resolver/provider access. Initialization/discovery validate lifecycle; typed calls must not perform an earlier provider-backed introspection of foreign arguments. Reuse shared helpers without duplicate dispatch algorithms; retain request-local identity only, never cache authorization across calls. Add ResolutionDigest to private identity type if necessary for Task4; new-format commands require complete matching proof and exact private target.
- [ ] Implement resource-rooted typed responses and adapters into current Reader/Commander. Reject old cloud selectors in new HTTP and MCP; no raw-provider fallback or arbitrary PromQL. Bounds/error sanitization and shorter caller deadlines preserved.
- [ ] Run full Infra go test ./... and go vet ./... once after focused TDD; commit/report exact constructor, JSON shapes, selectors and auth seams for Task4. This task does not enable/deploy the new handler.

### Task 4: Raptor authorization and resource runtime wiring

**Files:** Infra cmd/server/{main.go,proxy_startup.go}, metrics/prometheus.go, Makefile, s*.yaml and tests; Raptor internal/{agentaccess,adapters,approvals,backend,openapi} affected contract files/tests and cmd/backend/main.go. Active deployment templates only if needed to remove file requirements. No Gateway/harness changes until Task5.

**Interfaces:** Consume reviewed Task3 resource handler and service-only proof endpoint plus Task2 resolver/Task1 context. Raptor HTTPInfra consumes typed workload envelopes and private Proof. ResolveReplacement derives logical app through catalog context, obtains trusted Infra proof, stores ResolutionDigest in existing replacement_scope and freezes it across ALL attempt IDs of same request. Revalidate current exact identities per introspection after logical selector checks. Context route must remain catalog-only, proof route must not introspect agents.

- [ ] Write RED cross-boundary tests: code-only resource request accepted, revoked/foreign selection denied before provider-backed introspection, new attempt cannot reset digest/baseline, multiple app workloads denied, changed listener/RDS/app UID denied, legacy records readable but not mutation-authorizing, resource-only scoped Raptor MCP returns authorized dependency codes only.
- [ ] Add resource-based approval target {proxyCode}, interface db-proxy.scale-in, desiredCapacity parameter. Existing request frozen proof privately binds exact realization; test re-resolution before claim, retarget rejection, exact atomic claim/record and no replay. Historical ess.scale-in display/read remains. Prefer existing private request proof over new proof registries/tables; report any schema need before expanding storage.
- [ ] Replace production scope-file startup/packaging with fixed catalog client/resolver using existing private service credentials; remove private replacement-scope startup requirement. Build request-local Environment/ProxyMapping from resolver; existing runtime/policy consumes internal structs unchanged. Metrics obtains current resource environment/mapping through resolver, not global file map. Missing data/config fails closed; no automatic catalog population or IAM grant.
- [ ] Migrate Raptor HTTPInfra and browser discovery by converting typed resource response into existing private adapter DTO as needed; preserve exact restart UID/resourceVersion and no retry. Keep public Infra selectors logical.
- [ ] Run Infra full test/vet and affected Raptor catalog/backend/openapi/agentaccess/approvals/adapters/requests tests/vet with isolated fixture DB. Preserve unrelated duplicate files; broad-module limitation documented if relevant. Commit/report current contracts/startup requirements; no deployment.

### Task 5: Gateway/harness producer-consumer migration

**Files:** components/agent-harness/{gateway-observation-runner.mjs,gateway-observation-runner.test.mjs,pgcat-replacement-harness.mjs and corresponding tests}; components/agent-gateway/internal/runtime/{observation_proxy.go,live.go and affected tests}; Raptor execution/qualification view checks/tests; Infra cmd/mcp-probe and current contract docs/fixtures. Locate actual matching producer before edits; do not edit immutable released skill files.

**Interfaces:** Consume Tasks3-4 resource-only MCP arguments and pgproxyWorkload envelopes. Convert to existing private guard observation objects in one boundary adapter; provider IDs may remain internal evidence but never required model selectors. Update current qualification required-check list to internal account verification instead of cloud_identity_get tool. Existing historical qualification display remains readable.

- [ ] Add RED tests asserting exact outgoing resource args, nested workload parsing/type rejection, foreign code rejection, unchanged timestamp/sample semantics, request-bound logical approval schema, paused/resumed proof continuity, old skill contract explicit error rather than fabricated PASS.
- [ ] Implement coordinated producer/consumer changes and strict result adapters. Remove implicit raw provider read fallback from normal execution; diagnosis remains explicitly separate.
- [ ] Run focused Node tests, relevant Gateway Go tests+vet, Raptor view tests; once stable run full affected harness suite once. Commit/report; no live Raptor request or replacement flag mutation.

### Task 6: Coordinated integration and release readiness

**Files:** current components/infra-api/README.md, docs/setup/infra-api-read.md, active refactor spec/plan, setup contract fixtures and existing Terraform infra-api-read module tests/README only if permission analysis warrants. No IAM policy grant without separately reviewed exact design.

**Interfaces:** Tasks1-5 current contracts, source/build receipts and explicit permission requirements.

- [ ] Add/run integration contract test proving Raptor catalog code -> Infra resolution/workload -> Gateway observation, foreign/retarget denial, exact private approval claim, no scope.json in code package/startup, and no discovery recursion.
- [ ] Verify all current endpoint/tool inventories; report any deliberate legacy service diagnostic or required migration. Do not claim fully refactored if an active cloud-selector bypass survives.
- [ ] Run coordinated appropriate suites once; package source via existing build paths. Review IAM delta: if deployed policy lacks required read action, document exact least-privilege Terraform change/release blocker rather than borrowing credentials. Current owner authorization does not silently expand IAM.
- [ ] Commit current docs; perform one whole-branch review plus bounded fix wave under SDD. Prepare PR superseding held old image pin release; standing owner authorization covers push/merge but deployment waits coherent image receipts, catalog relationship populated, permissions verified, replacement disabled and idle readiness.
- [ ] Only after prerequisites pass, read-only live qualification with fresh evidence/baseline/harmless approval/pause-resume/cleanup. Full 2→4→2 remains NOT PASS until separate complete admission and actual live evidence pass. No native closure scope expansion under this plan.
