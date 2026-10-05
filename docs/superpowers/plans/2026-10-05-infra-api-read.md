# Infra API Read Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for the preserved native execution method. Implement task-by-task with RED→GREEN tests and one independent whole-branch review.

**Goal:** Ship authenticated, scoped cloud discovery/status through Go Infra API and prove one real sandbox-to-gateway-to-FC read.

**Architecture:** Infra API is an independent stateless Go module. Provider adapters return allowlisted records to HTTP handlers; Raptor uses an HTTP reader without importing Infra API storage or SDKs. Serverless Devs owns FC packaging; Terraform owns ingress and roles.

**Tech Stack:** Go >=1.25, net/http, official Alibaba Cloud Go SDKs/credentials provider, Kubernetes client, Serverless Devs fc3, existing Aliyun Terraform provider family.

**Spec:** `docs/superpowers/specs/2026-10-05-infra-api-read-design.md`.

## Global Constraints

- One public repository; Singapore `ap-southeast-1`; native execution retained.
- Probe allocation at most RMB20 inside cumulative Aliyun ceiling RMB1000; preflight current spending and selected-edition quote before creation.
- Independent `components/infra-api` module; no Raptor/Gateway storage import, database, operation journal, arbitrary shell or business workflow.
- Read-only slice. No restart/scaling/protection/deregistration mutation or new ACK/RDS/PgCat workload.
- Standard Basic Auth locally; dedicated `X-Infra-Authorization` caller header through the documented traditional FC3 HTTP gateway route.
- HTTPS remotely; protected FC trigger. Never substitute anonymous FC ingress to obtain a passing result.
- Read-only function RAM role, temporary credentials, explicit private environment/account/cluster allowlist; no provider secrets in logs, responses, Git or package.
- Missing configuration/provider failures are errors, not empty inventories. Fixtures are simulated; actual provider observations are live.
- Subsequent cloud hosting of Raptor/approval verification is outside this slice. Do not call Mac loopback from FC.
- Preserve PR #5. At execution create a separate branch/worktree from this approved plan commit; do not merge or overwrite the existing branch.

## Review Focus

- A provider page repeats its token or changes total count: fail incomplete discovery instead of looping or returning a partial success (Task 2).
- Gateway overwrites Authorization or forwards duplicate caller credentials: fail authentication rather than accepting a header intended for FC IAM (Tasks 1, 4).
- A Kubernetes selector matches unrelated pods: constrain pod status to the selected Deployment/ReplicaSet owner UID, not only shared labels (Task 2).
- An environment endpoint redirects to another host: never forward Raptor caller credentials across redirects (Task 3).
- A timed-out deployment/cleanup has an uncertain outcome: record exact owned IDs and reconcile before retry/delete; never destroy pre-existing resources (Task 5).

## Task 1: Scoped HTTP service contract

**Files:** Create `components/infra-api/go.mod`, `cmd/server/main.go`, `internal/api/{server.go,server_test.go}`, `internal/domain/{models.go,errors.go}`, `internal/scope/{config.go,config_test.go}`.

**Interfaces:**
- Produces `domain.Environment {Code,AccountID,Region,ClusterID string}` and `domain.Target {EnvCode,AppCode,ClusterID,Namespace,Name,UID string}`.
- Produces `domain.Deployment` JSON fields identical to `components/raptor/internal/adapters/infra.go`; `domain.Status` uses the existing DeploymentStatus fields plus allowlisted pod summaries.
- Produces `api.Reader` with `Identity(context.Context,domain.Environment)(string,error)`, `Deployments(context.Context,domain.Environment,string,string)([]domain.Deployment,error)` and `Status(context.Context,domain.Environment,domain.Target)(domain.Status,error)`.
- Produces `scope.Load(path string)(map[string]domain.Environment,error)` and `api.New(reader Reader,envs map[string]domain.Environment,username,password,authHeader string)(http.Handler,error)`.

- [ ] **Step 1: Write tests.** `TestHTTPAuthAndSelectors`: assert missing/wrong/duplicate/malformed auth→401; overwrite of standard Authorization cannot replace dedicated caller auth; GET /healthz reveals no provider data. `TestScopeAndIdentity`: unknown env→403; mismatched account→403; valid match→200 with no account/token fields. `TestDiscoveryErrors`: provider failure→503, actual empty list→200 data=[], unknown/duplicate selectors→400. `TestConfigRejectsInvalidScopes`: duplicate env, non-Singapore region, empty account and credentials embedded in config→rejected.
- [ ] **Step 2: Run RED.** `cd components/infra-api && go test ./internal/api ./internal/scope`; assertions/absent implementation fail, not an unrelated environment issue.
- [ ] **Step 3: Implement interfaces and executable.** Route only the four specified GET paths. Bound query fields to 256 bytes and total raw query to 4096; reject unknown/duplicate fields and unsupported kind. Use constant-time credential comparisons, fixed safe error envelopes and HTTP deadlines. Default local listen 127.0.0.1:8875; FC mode binds configured port. Load secrets privately, never log them.
- [ ] **Step 4: Verify GREEN/full module.** `go test -race ./... && go vet ./...`; no cloud API calls in tests.
- [ ] **Step 5: Commit.** `feat(infra-api): add scoped authenticated read contract`.

## Task 2: Actual cloud readers and safe Kubernetes projections

**Files:** Create `internal/provider/{aliyun.go,rds.go,ess.go,ack.go,kubernetes.go,pagination.go,provider_test.go}` and module lock file.

**Interfaces:** Consumes Task 1 Reader/Environment/Target/Deployment/Status. Produces `provider.New() (api.Reader,error)` using official credential-chain and SDK clients; provider construction accepts test transport/client factories without public arbitrary endpoint overrides.

- [ ] **Step 1: Write tests.** `TestProviderDiscoveryScopeAndPagination`: multi-page RDS/ESS results include only exact env/code tags, ESS includes target-db-code; duplicate/repeated token, pagination beyond 100 pages or inconsistent totals→explicit error, never partial success. `TestDeploymentAndPodIdentity`: wrong cluster/UID/app label→refusal; same labels with another owner UID never appear in pod summaries. `TestProviderSecretProjection`: kubeconfig, certificate, raw manifest/env and SDK errors never appear in result. `TestMissingACKIsNotEmpty`: absent registered cluster→NotConfigured. `TestIdentityUsesTemporaryCredentialClient`: STS identity uses configured provider and account scope.
- [ ] **Step 2: Run RED.** `go test ./internal/provider -count=1`.
- [ ] **Step 3: Implement adapters.** Check primary SDK/API docs, resolve and pin SDK versions in go.mod/go.sum. Use STS GetCallerIdentity, RDS DescribeDBInstances plus tag reads as required, ESS DescribeScalingGroups plus tags, and ACK DescribeClusterUserKubeconfig with explicit 15-minute temporary duration. Keep kubeconfig only in memory, refresh before expiry, require selected cluster's read-only RBAC and validated HTTPS API endpoint/CA. Do not execute kubeconfig exec plugins or permit insecure TLS. List Deployment across namespaces with label selector; status retrieves exact object and validates identity. Resolve ReplicaSet/Pod ownership by UID, paginate with the shared bounded helper and project fields only. Check total page completeness; abort on errors.
- [ ] **Step 4: Verify GREEN/full module.** `go test -race ./... && go vet ./...`; all provider tests use injected synthetic responses with explicit simulated evidence.
- [ ] **Step 5: Commit.** `feat(infra-api): add scoped cloud discovery readers`.

## Task 3: Raptor HTTP reader integration

**Files:** Create `components/raptor/internal/adapters/{infra_http.go,infra_http_test.go}`; modify `components/raptor/cmd/backend/main.go`; add setup configuration example without credentials.

**Interfaces:** Consumes existing `adapters.InfraReader`, `Deployment`, `DeploymentStatus`, domain.Environment/Object/RestartTarget and Task 1 wire envelopes. Produces `adapters.HTTPInfra {Username,Password,AuthHeader string; Client *http.Client}` implementing `ListDeployments(context.Context,domain.Environment,domain.Object)([]Deployment,error)` and `GetDeploymentStatus(context.Context,domain.RestartTarget)(DeploymentStatus,error)` with a configured environment resolver for the latter. No RestartDeployment method or mutation wiring in this slice.

- [ ] **Step 1: Write tests.** `TestInfraHTTPContract`: selected env/object map to exact read query; dedicated-header credentials come from runtime config; valid empty differs from 503. `TestInfraNoRedirectCredentialLeak`: redirect destination receives no call/credential; remote HTTP or credential-bearing URL rejected, loopback development allowed. `TestInfraScopeResponse`: wrong env/object/cluster/evidence and malformed envelope rejected. Missing secret configuration leaves reader unavailable rather than switching to fixtures.
- [ ] **Step 2: Run RED.** `cd components/raptor && go test ./internal/adapters -count=1`.
- [ ] **Step 3: Implement reader/wiring.** Resolve endpoint from the selected environment's `infraApiUrl`; secret contents remain in private backend runtime config. Explicit simulation mode retains FixtureInfra. Configure bounded HTTP client with redirects disabled and safe fixed errors. Validate response scope before returning data; accept only simulated/live evidence labels. Do not alter direct restart behavior or claim a live restart adapter.
- [ ] **Step 4: Verify GREEN/regressions.** Raptor and Infra API race suites/vet; existing Node and Python suites with the prepared runtime. Confirm no credentials enter MCP environment context.
- [ ] **Step 5: Commit.** `feat(raptor): connect read-only Infra API discovery`.

## Task 4: FC3 package, roles and gateway ownership

**Files:** Create `components/infra-api/{s.yaml,Makefile,README.md}`, `internal/packagecontract/package_test.go`, `terraform-module/infra-api-read/{main.tf,variables.tf,outputs.tf,versions.tf,tests/contract.tftest.hcl}`, `infra-terraform/environments/poc-sg-infra-api/{main.tf,variables.tf,outputs.tf,versions.tf}`.

**Interfaces:** Consumes Task 1 executable and Task 2 role actions. Produces a static linux/amd64 FC artifact, external secret/scope configuration, Terraform-owned read/invocation roles and gateway route outputs. Serverless Devs owns function/trigger only. Terraform consumes the deployed function trigger URL/name; it does not create an FC function.

- [ ] **Step 1: Write tests.** `TestFC3PackageBoundary`: runtime/start/port/code path agree, IAM HTTP trigger, no provisioned warm instances, no credential literals, no Mac loopback provider URL. Terraform contract asserts Singapore, role actions exclude mutation/wildcard Action, gateway forwards X-Infra-Authorization and uses FC3 backend with invocation role. Inventory ownership/cleanup instructions include exact IDs and separate tool owners.
- [ ] **Step 2: Run RED.** `go test ./internal/packagecontract`; `terraform test` with mocked provider and contract fixtures; initially absent package/resources fail.
- [ ] **Step 3: Implement verified configuration.** Check FC3 schema and pinned Aliyun provider for actual FC3 backend fields, validate CLI/component versions and lock them in setup receipt. Build CGO_ENABLED=0 GOOS=linux GOARCH=amd64; use documented custom runtime, 512MB, 30-second function timeout, concurrency cap 2, no warm reservation. Secret values are injected outside public configuration. Terraform read role includes only required STS/RDS/ESS/ACK reads, scoped resources where supported; record account-level list limitations. Add selected cluster's Kubernetes read-only binding only when the real cluster exists. If provider cannot express protected FC3 gateway integration, record that concrete limitation and stop live creation rather than ship old FC1 assumptions.
- [ ] **Step 4: Verify GREEN.** Package contract tests, static binary artifact inspection, `s` configuration/schema validation, Terraform fmt/validate/tests. No apply during local contract verification.
- [ ] **Step 5: Commit.** `build(infra-api): package FC3 read service and scoped ingress`.

## Task 5: Controlled live probe, receipt and final review

**Files:** Create `docs/setup/{infra-api-read.md,infra-api-read-acceptance.md}` and `components/infra-api/scripts/read-probe.py`. Private execution inventory/credentials stay ignored under `.raptor-local/infra-api-read/` with directory0700/files0600.

**Interfaces:** Consumes deployed ingress, scoped function role, dedicated caller auth mode, existing sandbox access and explicit temporary-resource inventory. Produces a sanitized acceptance receipt and separate draft PR. No receipt fields imply live ACK readiness when no cluster exists.

- [ ] **Step 1: Write tests.** `TestProbeClassifiesAuthenticationAndCloudRead`: fake health alone never passes; missing/wrong credentials fail, valid identity response requires live evidence/account match; direct unauthenticated FC call must fail. `TestCleanupOwnsOnlyRecordedResources`: absent/ambiguous IDs or incomplete deployment acknowledgement prohibit blind destroy/recreate. No auth headers or resource secrets appear in probe output.
- [ ] **Step 2: Run RED.** Test probe fixtures locally; failures establish classification/ownership behavior.
- [ ] **Step 3: Implement probe then execute preflight.** Use existing authorized Aliyun profile privately. Inspect current cumulative spending, Singapore edition availability/quote and existing resources read-only. Reconcile deployment prerequisites, estimate bounded total within RMB20/RMB1000 and record cleanup IDs as resources are created. Apply only this Terraform scope and deploy the FC artifact using Serverless Devs. Supply caller credential privately to the existing sandbox and run positive/negative probes. ACK-less identity proof is labelled as such; do not create extra workloads. Reconcile ambiguous apply/deploy outcomes before retry. Remove temporary owned probe resources after evidence collection and confirm deletion/billing state. If budget, region, protected integration or sandbox auth is unavailable, record the exact blocker and preserve working local implementation; never replace live acceptance with fixture success.
- [ ] **Step 4: Verify GREEN and review.** Three Go modules race suites/vet, appropriate existing Node/Python suites, Terraform/package/probe tests and sanitized live receipt. Dispatch one independent whole-branch reviewer under native workflow, grade findings by user impact and address substantive findings with focused RED→GREEN tests. Record deferred minors/rulings. If live acceptance is blocked, PR/receipt explicitly say incomplete.
- [ ] **Step 5: Commit/open draft PR.** `docs(infra-api): record authenticated read acceptance and limits`; push separate branch, create/attach a draft PR, retain worktree for feedback. Do not merge PR #5 or this PR.

## Plan self-review

Coverage: wire/auth/error contracts→Task1; identity/tag discovery/ACK/status/pagination/secret projection→Task2; Raptor consumer→Task3; tool ownership/roles/package/gateway→Task4; spending/preflight/live negative tests/cleanup/receipt→Task5. No mutation requirement is silently implemented or removed. New exported names and consumer shapes agree; existing DeploymentStatus is preserved with additive pod projection only on Infra API output. Five review-focus cases have named regressions in their owning tasks. Ordinary tests are synthetic, cloud acceptance is separate. Native execution is retained, but implementation waits for this written-plan review.
