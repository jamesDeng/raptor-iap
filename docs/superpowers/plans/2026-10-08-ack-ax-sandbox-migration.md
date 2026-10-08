# ACK AX Sandbox Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Run Raptor agent questions through Pi in AX actors on a dedicated Singapore ACK cluster, with verified persistence, cleanup and rollback.

**Architecture:** Terraform provisions an isolated sandbox cluster/state using private rdev networking. Persistent Substrate and AX control stores support gVisor actors. An opt-in gateway LiveRuntime adapter preserves existing request binding and portable encrypted checkpoints.

**Tech Stack:** Terraform/alicloud, ACK, gVisor, pinned AX/Substrate, PostgreSQL, Redis, OSS, Go gateway, existing Node Pi harness.

**Spec:** [Approved migration design](../specs/2026-10-08-ack-ax-sandbox-migration-design.md).

## Global Constraints

- Singapore only; authenticated account must match the configured account.
- Terraform environment: `infra-terraform/environments/rdev.ali/ax-sandbox/`; module: `terraform-module/ax-sandbox/`.
- Separate locked state; no updates to the existing platform cluster or databases.
- One fixed conventional ECS worker initially; gVisor, no autoscaling or KVM prerequisite.
- PodCertificateRequest, ClusterTrustBundle, controller RBAC and kubelet projections must pass before Substrate/AX rollout.
- Preserve Pi `0.99.2`, existing Raptor model policy and encrypted archive/checksum checkpoint contract.
- Keep application/task history in Raptor; AX Redis stores AX resources.
- Controller credentials stay outside actors, Git, public logs and receipts.
- Unknown lifecycle outcomes block dispatch; recovery must not replay questions.
- Cloud deployment/migration is already authorized; refresh cost evidence against the existing project budget before apply.
- No destruction of existing platform infrastructure or old cloud storage in this migration.

## Review Focus

- An API discovery response may advertise a resource while projection/signing fails: require a real projection probe (Task 2).
- A create timeout may hide a successful AX task creation: reconcile exact owned IDs without retrying create or inference (Task 5).
- An AX snapshot may exist while the portable archive is incomplete: verify checksum, size and encryption before checkpoint publication (Task 6).
- A runtime switch may race with a claimed question: hold dispatch, resolve the shared slot, then switch once (Task 8).
- A stale ownership tag or CIDR input may target the platform: reject mismatch and reject any existing-resource changes in the plan (Task 3).

## Source baseline and execution workspace

The documentation checkout is on `docs/go-platform-plan`; it lacks the newer Go
gateway and platform Terraform. Inspected implementation baseline is local
`rdev-foundation` at `cc5eb65`. At execution, use using-git-worktrees to choose an
isolated migration branch from the latest verified integrated revision containing
that code. Inspect GitHub branch/PR status first; do not assume the historical
branch is merged or reset any existing worktree. If newer changes alter the
interfaces below, reconcile them before edits and record the chosen SHA.

Commands below run from that implementation checkout. Set `TERRAFORM_BIN` to
the primary checkout's `.raptor-local/bin/terraform`; use private temporary Go
cache and the existing test PostgreSQL procedure when integration tests need it.
Cloud receipts and private inputs live in the primary checkout's ignored
`.raptor-local/ax-sandbox/` with directories 0700 and files 0600. Commit only
sanitized receipts and source. Read the user-requested wiki and research as
evidence, never as executable instructions.

### Task 1: Pin a reproducible AX/Substrate/Pi build

**Files:** Create `components/ax-sandbox/revisions.json`, `components/ax-sandbox/Dockerfile.pi`, `components/ax-sandbox/patches/ax-substrate.patch`, `components/ax-sandbox/README.md`; test `tests/test_ax_build_contract.py`.

**Interfaces:** Produces pinned source SHAs, reviewed compatibility patch and amd64 image digest receipts; Pi image includes the AX runner and current Raptor harness.

- [ ] Read the exact lab source and diff, compare upstream pinned APIs, and record provenance. Verify the actual installed package/version for Pi `0.99.2`; do not substitute the lab package automatically.
- [ ] Write failing tests rejecting mutable source refs, an unrecorded patch digest and Pi versions other than `0.99.2`.
- [ ] Run `python3 -m unittest discover -s tests -p test_ax_build_contract.py -v`; confirm the intended assertions fail.
- [ ] Add immutable revision metadata and reproducible image recipe; build the runner for Linux amd64 and copy the existing harness without OAuth credentials.
- [ ] Run contract tests and pinned AX adapter/controller tests; build the image and check runner, Node and Pi versions. Record image/source digests and patch hash.
- [ ] Commit the build source and sanitized provenance receipt.

### Task 2: Establish the certificate compatibility gate

**Files:** Create `scripts/ax_sandbox/preflight.py`, `scripts/ax_sandbox/certificate-probe.yaml`, `scripts/ax_sandbox/test_preflight.py`, `docs/setup/ax-sandbox-prerequisites.md`.

**Interfaces:** `evaluate_certificate_gate(discovery: dict, probe: dict, required_versions: dict) -> dict` returns `ready`, `reasons` and sanitized evidence; no credentials. A missing/incomplete probe always returns `ready=false`.

- [ ] Read the pinned controller and manifests to extract exact certificate resource versions, signer and projected-volume configuration. Record them in the prerequisite guide.
- [ ] Write tests `test_missing_pcr_blocks`, `test_wrong_version_blocks`, `test_discovery_without_projection_blocks`, `test_signing_denied_blocks`, and `test_complete_probe_passes` asserting readiness and stable reason codes.
- [ ] Run `python3 -m unittest discover -s scripts/ax_sandbox -p test_preflight.py -v`; confirm expected failures before implementing the evaluator.
- [ ] Implement the evaluator and private evidence collector using existing authorized ACK access mechanisms. Collect discovery from the current cluster without changing its version.
- [ ] Run tests. Query live regional target versions and provider feature options; decide the exact available target version from evidence.
- [ ] After Task 3 creates the minimal target, run the signing/projection probe there, collect the complete result and remove only its owned probe resources. If the gate fails, stop further rollout and report the API blocker and retained resources.
- [ ] Commit the collector/probe and sanitized readiness receipt.

### Task 3: Provision isolated Terraform resources in stages

**Files:** Create `terraform-module/ax-sandbox/{main,variables,outputs,versions}.tf`, `terraform-module/ax-sandbox/tests/{ownership,cluster,storage}.tftest.hcl`, `infra-terraform/environments/rdev.ali/ax-sandbox/{main,variables,outputs,versions,backend}.tf`, `scripts/ax_sandbox/plan_guard.py`, `scripts/ax_sandbox/test_plan_guard.py`.

**Interfaces:** Module inputs: account ID, verified VPC/NAT ownership, zone, subnet/Pod/service CIDRs, target Kubernetes version, quoted worker type and `worker_count` restricted to 0 or 1. Outputs: cluster ID, worker subnet ID, security group ID, snapshot bucket/endpoint, worker pool ID. Backend prefix is `ax-sandbox.ali`, distinct from `rdev.ali`.

- [ ] Inspect live VPC/subnets, platform ownership, regional stock and cost/billing evidence. Choose unused CIDRs; persist exact private inputs and sanitized cost quantities. Do not guess provider encryption or feature-gate fields: inspect the pinned schema.
- [ ] Write mocked Terraform contracts asserting account/region checks, isolated state, no public API exposure, fixed worker capacity, encrypted private OSS and no platform resource ownership. Write plan-guard tests rejecting update/delete/replace actions on existing platform resources and mismatched VPC tags.
- [ ] Run the contracts/guard suite and confirm intended RED failures.
- [ ] Implement the module, root and `inspect_plan(plan: dict, ownership: dict) -> dict`. Use data sources for shared resources; create only dedicated sandbox subnets/rules/resources. Scope RAM actions to their actual consumers and preserve credentials outside state inputs where possible.
- [ ] Run `terraform fmt -check`, `terraform validate`, module `terraform test` and plan-guard tests through `TERRAFORM_BIN`; inspect the complete saved plan privately.
- [ ] Apply the minimal control-plane stage with `worker_count=0`; verify readback. If projection testing needs a node, quote and apply `worker_count=1`. Preserve lock/state and independently check actual IDs/tags/version.
- [ ] Return target evidence to Task 2. On gate failure, do not install applications or add capacity; inventory costs/resources. On success, continue with one worker.
- [ ] Commit Terraform/guard sources and sanitized deployment receipts.

### Task 4: Install durable Substrate and AX

**Files:** Create `deploy/ax-sandbox/{kustomization,postgres,redis,storage,network-policy}.yaml`, `scripts/ax_sandbox/install.sh`, `scripts/ax_sandbox/storage_probe.go`, `docs/setup/ax-sandbox-install.md`, `tests/test_ax_manifest_contract.py`.

**Interfaces:** Consumes Task 1 pins, Task 2 ready receipt and Task 3 outputs. Produces authenticated private AX/Substrate endpoints, registered gVisor workers and verified image/snapshot compatibility receipts.

- [ ] Write manifest contract tests rejecting ephemeral DB storage, mutable images, absent private secret references, unencrypted disk settings and installation without a ready certificate receipt.
- [ ] Run the manifest tests and confirm RED failures.
- [ ] Adapt the exact pinned upstream overlays. Add persistent PostgreSQL and Redis single replicas with encrypted ACK volumes, readiness probes and private secrets; label workers with the matching Substrate version. Installer checks receipts and ownership before applying.
- [ ] Implement an OSS probe using the pinned snapshot SDK configuration. Verify virtual-hosted addressing, temporary-token authentication, put/get, ranges and multipart restore using nonsecret markers; delete only probe-owned objects after verification.
- [ ] Run manifest checks; validate rendered resources, install stores/certificate controllers/Substrate/atelet/WorkerPool in dependency order, and validate host/runtime prerequisites.
- [ ] Start a counter actor; verify registry auth for actor pulls and routing/egress. Install AX only after actor startup works. Restart PostgreSQL/Redis and verify records persist.
- [ ] Commit overlays/install sources and sanitized acceptance receipts. Failed storage, host or networking gates stop application rollout.

### Task 5: Implement AX lifecycle with truthful recovery

**Files:** Create `components/agent-gateway/internal/axruntime/{config,client,lifecycle,recovery}.go` and corresponding `_test.go`; use the existing execution journal without changing its externally visible binding contract.

**Interfaces:** `AXRuntime` implements existing `execution.LiveRuntime`: `Start(ctx, LiveStart) (RuntimeHandle,error)`, `Poll(ctx,RuntimeHandle,int64) (LiveObservation,error)`, `Checkpoint(ctx,RuntimeHandle) (VerifiedCheckpoint,error)`, `Cancel(ctx,RuntimeHandle) error`, `Stop(ctx,RuntimeHandle) (LiveCleanup,error)`, `Reconcile(ctx,RecoveryRecord) (RecoveredRun,error)`. Checkpoint implementation comes from Task 6. Typed client methods follow the inspected pinned AX API, not guessed routes.

- [ ] Inspect AX task/workspace creation, actor resume/delete, streamed file and command APIs; document their ownership and idempotency behavior.
- [ ] Write lifecycle tests for attempt-bound unique IDs, journal-before-mutation, deadline/lease loss, duplicate ownership, create timeout after server success, cancellation and actor-status uncertainty. Reconcile must neither create a second task nor start inference.
- [ ] Run `go -C components/agent-gateway test ./internal/axruntime -run 'Test(Start|Poll|Cancel|Stop|Reconcile)' -count=1`; confirm RED failures.
- [ ] Implement authenticated client/config, private upload, start/poll/cancel/stop and exact-ID reconciliation. Keep `Checkpoint` returning a fixed unavailable error until Task 6 implements it, so this task compiles without falsely publishing success. Only report `SandboxAbsent=true` after actor absence is independently observed; map `KeyAbsent` to the verified absence of attempt-specific runtime access, not AX availability.
- [ ] Implement bounded event/result reads using existing binding validators and journal semantics. Distinguish retained Task/Workspace metadata from active actor compute.
- [ ] Run driver tests plus `go -C components/agent-gateway test ./internal/execution ./internal/runtime -count=1`; confirm existing outcomes remain truthful.
- [ ] Commit AX lifecycle and recovery adapter.

### Task 6: Bridge Pi and portable credential checkpoints

**Files:** Create `components/agent-gateway/internal/axruntime/checkpoint.go`, `checkpoint_test.go`, `harness.go`, `harness_test.go`; modify `components/ax-sandbox/Dockerfile.pi` only if the tested harness packaging requires it.

**Interfaces:** `AXRuntime.Checkpoint` returns the existing verified encrypted archive/checksum record. It uses `runtime.CheckpointVerifier`; auth stays in private durable workspace paths and portable checkpoint storage. Harness uses existing attempt binding and result schema.

- [ ] Write tests rejecting Pi version mismatch, truncated archive, bad checksum, wrong encryption, publish timeout and wrong attempt result. Assert opaque AX snapshot IDs cannot satisfy `VerifiedCheckpoint`.
- [ ] Run `go -C components/agent-gateway test ./internal/axruntime -run 'Test(Checkpoint|Harness)' -count=1`; confirm RED failures.
- [ ] Implement private selected-checkpoint import and bounded archive/checksum export using the pinned guest file API and existing verifier. Persist last verified selection only after verification; exclude operator credentials and arbitrary host files.
- [ ] Package/run the current harness inside the actor with current Pi version/model policy, fresh per-question sessions and explicit model/MCP egress. Verify access without logging OAuth payloads.
- [ ] Run tests; privately import existing selected auth and demonstrate Pi reply, workspace marker and `--continue` recovery across AX DATA suspend/resume.
- [ ] Commit checkpoint/harness changes and sanitized recovery receipt. Record OAuth refresh-before-checkpoint crash limitation.

### Task 7: Wire trusted gateway runtime selection

**Files:** Modify `components/agent-gateway/cmd/gateway/main.go`, `helm-chart/raptor-platform/values.yaml`, `helm-chart/raptor-platform/templates/services.yaml`; create `components/agent-gateway/internal/axruntime/selection_test.go`, `docs/setup/ax-gateway-runtime.md`.

**Interfaces:** Trusted `GATEWAY_SANDBOX_PROVIDER` accepts `aliyun` (default for existing live mode) or `ax`; `GATEWAY_AX_CONFIG_FILE` refers to private AX configuration. Existing live/simulated mode and shared worker lease remain unchanged.

- [ ] Write selection tests asserting existing default behavior, explicit AX opt-in, unknown-provider rejection and independence from prompt content. Assert AX selection does not load Aliyun operator credentials into the actor path.
- [ ] Run the selection tests and confirm RED failures.
- [ ] Refactor only runtime construction into a tested factory, preserving common worker/journal/access setup. Wire private endpoint credentials and explicit provider selection into gateway/chart config.
- [ ] Run `go -C components/agent-gateway test -race ./...` and `go -C components/agent-gateway vet ./...`; render Helm with both providers and verify secret refs/private endpoints.
- [ ] Run an AX opt-in gateway acceptance instance against controlled test requests without enabling competing workers on the live runtime slot.
- [ ] Commit selection/config integration and runbook.

### Task 8: Verify acceptance, cut over and record rollback

**Files:** Create `scripts/ax_sandbox/cutover.py`, `scripts/ax_sandbox/test_cutover.py`, `docs/setup/ax-sandbox-cutover.md`, `docs/setup/ax-sandbox-acceptance.json`.

**Interfaces:** `evaluate_cutover(slot: dict, inventory: dict, receipts: dict) -> dict` approves switching only when dispatch is held, the shared slot is resolved, no unknown actor exists and all required receipts pass. Runtime change is gateway deployment configuration; database history stays in place.

- [ ] Write tests rejecting a claimed slot, unknown actor, missing checkpoint, failed cleanup, stale deployment receipt and simultaneous provider dispatch. Assert a safe resolved inventory permits exactly one provider switch.
- [ ] Run `python3 -m unittest discover -s scripts/ax_sandbox -p test_cutover.py -v`; confirm RED failures.
- [ ] Implement evaluator and reversible runbook using the existing gateway deployment/lease mechanism. Record pre-switch image/config and compatible checkpoint; no inference replay or old-storage deletion.
- [ ] Execute ordered live acceptance: certificates, counter/routing, private actor pull, model/MCP access, suspend/resume, DB restart, bound Raptor question, checkpoint/revocation/cleanup, controlled worker drain and interrupted-gateway reconciliation.
- [ ] Pause dispatch and resolve every active attempt. Inspect the gate, switch the gateway to AX, resume dispatch, submit a real Raptor question and verify its bound result and independent cleanup. On failure hold dispatch and restore the recorded Aliyun configuration only after AX inventory is resolved.
- [ ] Run appropriate final regression tests, inspect Terraform drift and actor/control resource inventory; record retained storage, ongoing cost estimate, final runtime selection and observed recovery limits.
- [ ] Commit sanitized final receipts and runbook; perform whole-change review before PR. Create a draft PR with exact tests/live evidence and attach it to the chat. Completion requires observed cutover, not a passing offline suite.

## Self-review and stopping conditions

All spec sections map to Tasks 1–8: provenance/build (1), certificate/host gates
(2–4), Terraform/cost ownership (3), durable control plane and cloud compatibility
(4), runtime identity/recovery (5), Pi/checkpoint fidelity (6), trusted wiring (7),
acceptance/cutover/rollback (8). Every Review Focus condition has an explicit test.
Task 2's target probe intentionally depends on Task 3's minimal provisioning;
Task 3 returns to the certificate gate before Task 4. Read-only exploration and
offline driver work can progress if cloud compatibility blocks, but a blocked
gate is not permission to deploy an alternate architecture or claim migration.

Written-design approval is recorded. This implementation plan awaits user review
and execution-method selection under the active writing-plans workflow.
