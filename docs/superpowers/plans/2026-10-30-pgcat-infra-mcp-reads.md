# PgCat Infra MCP Reads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development for parent implementation/review coordination; implementers follow test-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking. The owner has approved this bounded integration; the parent will dispatch one editor and review each unit.

**Goal:** Route normal ESS/NLB and PgCat metric observations through two fixed read tools on the existing Infra MCP service.
**Architecture:** Reuse existing Alibaba SDK and private ACK Prometheus transports, existing attempt credentials and exact request authorization. A transition-aware observation projection preserves current observer contracts while command guard snapshots remain unchanged. Gateway privately invokes MCP and injects reads at the shared observation boundary, including reads nested inside metrics/SQL probes.
**Tech Stack:** Go, existing Alibaba SDK/client-go, Node.js, installed `@earendil-works/pi-mcp` 1.3.0; no new dependencies.
**Spec:** [approved policy](../specs/2026-10-30-pgcat-connection-budget-design.md); [approved bounded routing proposal](../specs/2026-10-30-pgcat-infra-mcp-reads-proposal.md). The owner's subsequent approval covers precisely the two-tool integration described here.

## Global Constraints

- No new service, public HTTP route, cloud resource, IAM action, credential scheme, controller, cache or evidence framework; no deployment/live mutation in this source task.
- Preserve command Snapshot/ConnectedClients guard semantics, existing approvals, model tool catalogs, observation outputs and dedicated min=2/max=4 capacity checks.
- Observation `registered` means NLB weight>0; guard Snapshot Registered retains its current presence semantics. Only observations allow pending membership count to differ from desired.
- Keep Gateway observation timeout **20 seconds**, metric source freshness **15 seconds**, response freshness **30 seconds**; slow/unavailable reads fail closed without direct-cloud/direct-Prometheus fallback.
- Alibaba CLI remains explicit operator diagnosis only. Current Gateway command-shaped cloud requests actually execute SDK CallApi; eliminate that bypass from the normal read route.

## Review Focus

- Pending scale-out members absent from NLB stay pending without requiring metrics; members already in NLB do not gain that exemption (Units 1/3).
- Protected/InService ESS health and transitional NLB status/weight must retain separate meanings; bad/missing fields cannot become healthy defaults (Unit 1).
- Qualification permits the new reads while application-question/foreign/expired/revoked attempts cannot use them (Unit 2).
- Nested metrics/SQL calls and Infra outages cannot silently reach old cloud/Prometheus transports (Unit 3).
- MCP initialization, eight metric queries, cancellation and result sanitization must respect the existing whole-observation deadline (Unit 3).

---

### Task 1: Typed observations and existing provider composition

**Files:** Create `components/infra-api/internal/domain/proxy_observation.go`, `internal/provider/proxy_observation.go`, `internal/provider/pgcat_observation.go`; modify `internal/provider/aliyun.go`, `internal/provider/proxy_cloud.go`, `internal/api/server.go`. Paths after the first are relative to `components/infra-api/`.
**Tests:** New `internal/provider/proxy_observation_test.go`, `pgcat_observation_test.go`; extend `provider_test.go`; existing `proxy_snapshot_test.go`, `prometheus_test.go` and `proxy_sdk_test.go` remain regressions.

**Interfaces/DTOs:** Define `domain.ProxyReadTarget` with RequestID/EnvCode/ProxyCode/GroupID/ServerGroupID and matching lowerCamel JSON names. Define optional `api.ProxyObserver` with `ObserveProxy(context.Context, domain.Environment, domain.ProxyReadTarget, string) (domain.ProxyObservation, error)` and `ObservePgcatSeries(context.Context, domain.Environment, domain.ProxyReadTarget, string) (domain.PgcatSeriesObservation, error)`; do not expand existing Reader/Commander.
`domain.ProxyObservation` is an internal tagged union of `Fleet *FleetObservation`, `Backend *BackendObservation`, `Capacity *CapacityObservation`; exactly one branch is set, and Unit 2 exports that branch alone, never the wrapper.
`FleetObservation={groupId:string,observedAt:time.Time,instances:[]FleetNode}`; `FleetNode={instanceId:string,protectedFromScaleIn:bool,lifecycleState:string,healthStatus:string}`.
`BackendObservation={serverGroupId:string,listenerId:string,observedAt:time.Time,nodes:[]BackendNode}`; `BackendNode={instanceId:string,privateIp:string,status:string,registered:bool,healthy:bool}`.
`CapacityObservation={groupId:string,desiredCapacity:int,minSize:int,maxSize:int,observedAt:time.Time}`; DTO fields use explicit JSON tags and non-nil arrays.
`PgcatSeriesObservation={series:string,observedAt:time.Time,rows:[]PrometheusSample}`; `PrometheusSample` has `Metric map[string]string` and `Value []json.RawMessage`, tagged `metric`/`value` to preserve raw vector shape.
The eight exact `series` values are **up, upTimestamp, active, activeTimestamp, idle, idleTimestamp, waiting, waitingTimestamp**. Values map to `up` or `pgcat_pools_cl_{active,idle,waiting}`, with timestamp(...) only for Timestamp variants; selector is exactly job=pgcat and registered env/proxy/target-db labels. No input accepts PromQL, URL, node IDs or arbitrary metric names.

- [x] Add failing provider tests for all three branch shapes, zero/foreign/duplicate mapping, account mismatch, pagination/truncation, pending counts, all allowed ESS lifecycle values, NLB Available/Adding/Configuring/Removing, health/weight/IP validation, and preserved snapshot refusals.
- [x] Add failing metric-series tests proving exactly eight fixed scoped expressions, vector shape/size bounds, rejected unknown series/provider warnings/malformed data, and sanitized upstream failure. Stub query transport; do not query cloud.
- [x] Run `cd components/infra-api && go test ./internal/provider ./internal/api`; confirm new assertions fail for absent interfaces/projections, not fixture errors.
- [x] Implement observation methods on existing `*provider.reader`; reuse mapping/account/tag/database checks and typed `proxySDK` functions. Do not weaken guard helpers to admit transitions: separate observation enumeration from strict command Snapshot. Validate complete bounded pages; fleet lifecycle enum is InService/Protected/Pending/Pending:Wait/Standby/Stopped/Removing/Removing:Wait with Healthy/Unhealthy health; healthy requires serving lifecycle+ESS Healthy+Available NLB+positive weight+no non-normal health.
- [x] Refactor construction explicitly through `newReaderWithConfig(config func(string) *openapi.Config) (*reader,error)`: public New creates one existing credential/config closure, builder attaches identity/page/kube and a ProxyBackend using `cloudProxy(config)` plus that same reader's Identity. NewProxyBackend obtains that built reader's attached backend; remove its second credential construction. **Never call NewProxyBackend from New/builder**: current NewProxyBackend calls New, so that would recurse.
- [x] Implement fixed series with this reader's existing `PrometheusQuery(ctx,env,query,evaluation)` and existing fixed ACK Service path, after account/mapping/pin validation. Return bounded raw vectors; retain current JavaScript freshness/coverage validator and command metrics guard unchanged.
- [x] Run `cd components/infra-api && go test ./internal/provider ./internal/api ./internal/metrics ./internal/commands`; require PASS and review diff for guard changes before Unit 2.

### Task 2: Existing MCP exposure and both authorization boundaries

**Files:** Modify `components/infra-api/internal/api/mcp.go`, `operations.go`, `agent_authorization.go`, `service_authorization.go`; modify `components/raptor/internal/agentaccess/replacement_scope.go`. Add `api/proxy_observation_test.go`, extend `api/agent_authorization_test.go`, `api/mcp_test.go`, and Raptor `replacement_scope_test.go`.
**Consumes:** Unit 1 optional ProxyObserver, ProxyReadTarget, tagged state union and PgcatSeriesObservation.
**Produces:** `proxy_observation_get({requestId,envCode,proxyCode,groupId,serverGroupId,kind})`, kind=fleet/backend/capacity; `proxy_pgcat_metrics_get({requestId,envCode,proxyCode,groupId,serverGroupId,series})`, series=Unit 1 enum. Every field required; strings only, duplicates/unknowns rejected, additionalProperties=false. Existing `/mcp` returns structuredContent `{data:branch}` or `{data:PgcatSeriesObservation}` plus matching text; errors retain current sanitized codes.

- [ ] Add failing MCP tests for both exact schemas/envelopes, all enum values, duplicate/extra/missing selectors and read availability without Commander/approval/metrics runtime configuration. No public HTTP route or Terraform route is added.
- [ ] Add failing paired authorization tests for exact request/env/proxy/group/backend identity, application-question denial, qualification/replacement read allowance, scope changes, expiry/revocation and service Basic read allowance. Test registered target-db and ACK cluster mismatches against authenticated replacement scope/binding: refuse before SDK/query dispatch. Test that raw kind/series cannot select arbitrary operations.
- [ ] Run `cd components/infra-api && go test ./internal/api` and `cd components/raptor && go test ./internal/agentaccess`; require expected new assertion failures.
- [ ] Implement `func (o operations) proxyRead(ctx context.Context, tool string, args map[string]string) (any,error)` for these MCP registrations, with optional-interface NotConfigured handling, existing scope/timeouts and provider error sanitization. Authorize using the logical tool name as the existing Authorizer path key; add exact named cases to agentSelectors/ServiceAuthorizer, not HTTP routing/commandSpecs. Use `proxy_*` names to avoid current `db_proxy_` mutation visibility prefix.
- [ ] Add exact named-read cases in Infra `AgentIdentity.permits`/`agentSelectors` and Raptor `ReplacementSelectorsAllowed`: normalize only requestId/envCode/proxyCode/groupId/serverGroupId for introspection, validate finite kind/series before normalization; both sides bind all five values to the current replacement identity. Listener/port/database come from Infra mapping, never caller input. Preserve current re-introspection and unchanged binding checks.
- [ ] In the existing dispatcher, resolve exactly one registered mapping and compare it with the authenticated agent identity after Authorize's fresh introspection: environment ClusterID equals binding and replacement application cluster, mapping TargetDBCode equals replacement TargetDBCode, and mapping code/group/backend equal the scoped pins. Agent principal must have its agentAuthorizer identity; missing identity refuses. Service Basic follows current service policy. No caller targetDB/cluster override or new authorization scheme; mismatched catalog/config refuses before provider calls.
- [ ] Add both names to ServiceAuthorizer's read policy without authorizing unrelated commands; keep attempt-auth path independent of service-only scope. Register with readOnlyHint=true; annotations never replace server authorization.
- [ ] Run both Unit 2 package suites plus `cd components/infra-api && go test ./...`; require PASS and review exact auth cases before Unit 3.

### Task 3: Gateway private MCP reads and one injected observer boundary

**Files:** Modify `components/agent-gateway/internal/runtime/observation_executor.go`, `observation_proxy.go`; modify `components/agent-harness/gateway-observation-runner.mjs`, `observation-tools.mjs`. Extend Gateway `observation_proxy_test.go`; add `observation_executor_test.go`; extend harness `gateway-observation-runner.test.mjs`, `observation-tools.test.mjs`. A narrow Gateway Dockerfile staging change must package the existing locked MCP runtime; reuse the current offline image-import CI gate, without adding a dependency or smoke framework.
**Consumes:** Unit 2 tools via `in.Access.InfraMcpURL` and existing credential; private runner input `infra:{url,credential},deadlineAt` supplied from trusted LiveStart and Go context deadline only.
**Produces:** `createObservationTools({...existingOptions,readCloud,readPgcatSeries})`, where `readCloud({kind}):Promise<original projection>` and `readPgcatSeries(series):Promise<PrometheusSample[]>`; normal runner requires both callbacks. Local explicit operator diagnostics may keep old transport separately; normal missing callback always fails.

- [ ] Add failing runner tests proving top-level fleet/backend/capacity, nested pgcatClients and SQL probe membership/address reads all call fixed MCP tools, never cloud probe/CLI. Test Infra auth/transport/malformed/isError failures with zero fallback calls and unchanged observation shapes; retain pending absent/present, pool/duplicate/scope/freshness cases.
- [ ] Add Gateway tests that private input uses trusted access URL/credential, rejects missing/mismatched access, never returns credentials, and cancellation/late results cannot escape. Include secret-redaction checks for full header, Basic encoding, agent:credential and raw credential.
- [ ] Run targeted Go/Node suites; confirm new tests fail because routing is absent before implementation.
- [ ] Verify installed `node_modules/@earendil-works/pi-mcp/dist/{client,transports/streamable-http,index}.d.ts` again. Inspection found real exports `McpClient({name,version,requestTimeoutMs})`, `connect(transport)`, `callTool(name,args,{signal,timeoutMs})`, `close()` and `StreamableHttpTransport({url,headers,fetch,openGetStream:false,maxMessageBytes,reconnect:{maxRetries:0}})`. Use these APIs, not hand-written JSON-RPC/guessed dependencies.
- [ ] Create one SDK client/transport per runner observation; privately set existing X-Infra-Authorization Basic agent credential. Fetch must enforce the exact trusted URL, no redirects, bounded responses and the remaining **single 20-second whole-observation deadline**, including initialize and queries; pass call signal/remaining timeout, close in finally. No auth recovery, reconnect or retry. Decode structuredContent.data or matching text envelope, reject isError/wrong series/shape/secrets and normalize failures to InvalidObservation.
- [ ] Inject cloudRead at `tools.cloudRead` so internal metrics/probe calls share it. PgCat's current fixed query callback maps only its exact eight generated expressions to series and obtains rows from readPgcatSeries. Keep existing pending-row validation/readPgcatClients; no result cache and no change to traffic metric/deployment/SQL transports beyond their nested cloud reads.
- [ ] Preserve current response fields: pgcatClients observedAt/responseAt/sourceAt/latestResponseAt/latestSourceAt/nodes/pendingInstanceIds; nodes keep connectedClients and per-pool active/idle/waiting. Keep actual fleet/backend timestamps; do not restamp stale source observations as fresh.
- [ ] Run `cd components/agent-harness && node --test gateway-observation-runner.test.mjs observation-tools.test.mjs pgcat-metrics.test.mjs observation-proxy.test.mjs` and `cd components/agent-gateway && go test ./internal/runtime`; require PASS, including a timeout fixture that fails closed at the whole deadline and 15/30-second freshness regressions.
- [ ] Run full Infra API suite, Raptor agentaccess suite and `cd components/agent-harness && node --test *.test.mjs` (package.json has no npm test script). Review final diff: no normal bypass/fallback, no command guard or evidence model changes, no credentials in output, no Terraform/resource/dependency changes. Parent records each unit's verification and review before final completion.

## Remaining risks and handoff

Source inspection does not verify deployed Infra permissions, fixed metrics Service/RBAC reachability or live eight-query latency; no new permissions are proposed. SDK initialization overhead and the existing 20-second process boundary can cause legitimate slow reads to fail closed. Keep existing unused observer credential issuance unchanged in this bounded integration; removing it is separate cleanup. Implementation is already approved; proceed under the parent's single-editor dispatch without another approval gate.

## Execution notes — 2026-10-30

Task 1 completed at `c0cd853` after independent review and one shared-validator refactor. Full Infra Go suite/vet passed at `0f60c57`; focused observation/Snapshot tests passed again after the behavior-preserving refactor. Task 2 is in test-first implementation. Task 3 is prepared but awaits Task 2 review. The existing Gateway image has Node and source but no node_modules, so the required narrow packaging change uses unchanged lockfile dependencies. Local Docker is unavailable; the existing Platform images build and offline import gate must verify the produced image. No cloud operation or live POC result follows from these source checks.
