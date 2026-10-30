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

### Task 3: Resource contracts, runtime configuration and authorization

**Files:** Modify Infra internal/api/{mcp.go,operations.go,commands.go,agent_authorization.go,server.go,service_authorization.go}, domain/{commands.go,proxy_observation.go}, cmd/server/{main.go,proxy_startup.go}, metrics/prometheus.go, Makefile, s*.yaml and associated tests; Raptor internal/{agentaccess,adapters,approvals,backend,openapi} affected contract files/tests and cmd/backend/main.go. Create focused resource_contract files instead of duplicating dispatch algorithms.

**Interfaces:** Consume Task2 resolver and service-only proof. Public signatures/results exactly Spec Agent-facing contracts. Domain internal command structs retain exact provider targets only after server-side resolution. New HTTP and MCP operations share decoding and resolution. Raptor ResolveReplacement derives logical app from Task1 context and trusted Infra service proof; saved replacement scope gains ResolutionDigest. Service proof read must not require attempt introspection. Add bounded request-scoped resource/dependency discovery through existing Raptor MCP, not arbitrary catalog access.

- [ ] Write RED cross-boundary tests: env/code-only request accepted, cloud selector injection rejected on HTTP/MCP, unknown/duplicate fields rejected, unauthorized resource produces zero provider reads, initial proof avoids recursion, revoked/changed target denied, app question access isolated, qualification cannot mutate, old records readable but cannot authorize new-format commands, multiple dependent Deployments denied, new attempt ID cannot rebind a request or reset its baseline, logical selectors denied before provider-backed introspection.
- [ ] Pin resource approval target/proof in private storage and claim path; test same-code retarget after approval denies submission and original action cannot replay. Use db-proxy.scale-in resource interface; preserve historical ess.scale-in display/read behavior only.
- [ ] Replace production scope-file startup/packaging with fixed catalog client/resolver config using existing credentials; remove static replacement scope startup. Reuse internal policy/runtime by constructing request-local Environment/ProxyMapping and exact Target after authorization. Metrics client resolves matching resource context per operation rather than global file map. Missing configuration/lookup explicitly fails closed.
- [ ] Migrate Raptor Infra HTTP adapters and normal browser discovery to typed resource workloads. Preserve exact restart UID/resourceVersion/no retry; support logical workloadKey resolution without arbitrary namespace override.
- [ ] Run Infra full go test ./... + go vet ./...; Raptor affected catalog/backend/openapi/agentaccess/approvals/adapters tests and vet with fixture DB where required. Commit/report current contracts and any consumer-facing migration errors. No mixed deployment.

### Task 4: Gateway/harness producer-consumer migration

**Files:** components/agent-harness/{gateway-observation-runner.mjs,gateway-observation-runner.test.mjs,pgcat-replacement-harness.mjs and corresponding tests}; components/agent-gateway/internal/runtime/{observation_proxy.go,live.go and affected tests}; Raptor execution/qualification view checks/tests; Infra cmd/mcp-probe and current contract docs/fixtures. Locate actual matching producer before edits; do not edit immutable released skill files.

**Interfaces:** Consume Task3 resource-only MCP arguments and pgproxyWorkload envelopes. Convert to existing private guard observation objects in one boundary adapter; provider IDs may remain internal evidence but never required model selectors. Update current qualification required-check list to internal account verification instead of cloud_identity_get tool. Existing historical qualification display remains readable.

- [ ] Add RED tests asserting exact outgoing resource args, nested workload parsing/type rejection, foreign code rejection, unchanged timestamp/sample semantics, request-bound logical approval schema, paused/resumed proof continuity, old skill contract explicit error rather than fabricated PASS.
- [ ] Implement coordinated producer/consumer changes and strict result adapters. Remove implicit raw provider read fallback from normal execution; diagnosis remains explicitly separate.
- [ ] Run focused Node tests, relevant Gateway Go tests+vet, Raptor view tests; once stable run full affected harness suite once. Commit/report; no live Raptor request or replacement flag mutation.

### Task 5: Coordinated integration and release readiness

**Files:** current components/infra-api/README.md, docs/setup/infra-api-read.md, active refactor spec/plan, setup contract fixtures and existing Terraform infra-api-read module tests/README only if permission analysis warrants. No IAM policy grant without separately reviewed exact design.

**Interfaces:** Tasks1-4 current contracts, source/build receipts and explicit permission requirements.

- [ ] Add/run integration contract test proving Raptor catalog code -> Infra resolution/workload -> Gateway observation, foreign/retarget denial, exact private approval claim, no scope.json in code package/startup, and no discovery recursion.
- [ ] Verify all current endpoint/tool inventories; report any deliberate legacy service diagnostic or required migration. Do not claim fully refactored if an active cloud-selector bypass survives.
- [ ] Run coordinated appropriate suites once; package source via existing build paths. Review IAM delta: if deployed policy lacks required read action, document exact least-privilege Terraform change/release blocker rather than borrowing credentials. Current owner authorization does not silently expand IAM.
- [ ] Commit current docs; perform one whole-branch review plus bounded fix wave under SDD. Prepare PR superseding held old image pin release; standing owner authorization covers push/merge but deployment waits coherent image receipts, catalog relationship populated, permissions verified, replacement disabled and idle readiness.
- [ ] Only after prerequisites pass, read-only live qualification with fresh evidence/baseline/harmless approval/pause-resume/cleanup. Full 2→4→2 remains NOT PASS until separate complete admission and actual live evidence pass. No native closure scope expansion under this plan.
