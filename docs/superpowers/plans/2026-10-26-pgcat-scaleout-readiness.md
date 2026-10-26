# PgCat Scale-Out Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent new PgCat nodes from entering NLB before their startup and monitoring have had time to settle, and let the native agent wait safely through that transition.

**Architecture:** Terraform adds a 420-second scale-out lifecycle hold to the existing PgCat ESS module. AX observations represent known Pending/Adding states without turning them into terminal errors. A persisted request-bound readiness witness unlocks post-expansion mutations only after exact four-node backend, metric and SQL evidence. A disabled 2→3 rehearsal validates the timing before another full paid request.

**Tech Stack:** Terraform 1.13.3, Alibaba provider 1.293.0, Node.js AX Pi observation tools, existing Raptor/ESS/NLB/GitOps workflow.

**Spec:** `docs/superpowers/specs/2026-10-26-pgcat-scaleout-readiness-design.md`

## Global Constraints

- Keep `replacementEnabled=false` until a separately gated full attempt; preserve GPT-6 Luna and immutable `skills-v0.4.0`.
- Keep exact `rdev.ali`/`DP00001`/group `asg-t4necb4dfmu6o3z2rn0g`/NLB server group `sgp-xs5s42tihtl6ja2a67` runtime scope.
- Cloud provisioning is Terraform/GitOps only. PR Terraform plan must post a sanitized comment; reviewed apply must preserve existing resource addresses/state and the protected two-node baseline.
- The collector's every-backend scrape, raw triplet, SQL failure and complete-window rules stay strict; no grace period or retroactive PASS.
- A mutation or lifecycle result with unknown acknowledgement is never replayed. Stop and reconcile exact provider state.
- CNY 1,800 net whole-POC ceiling; use today's existing billing check, no second billing query on 2026-10-26. No paid attempt starts after 21:30 Shanghai; handoff by 22:30.

## Review Focus

- An NLB `Adding`, `Configuring` or `Removing` row must be returned as unhealthy transition data; an unknown status or foreign server must still fail.
- A Pending unregistered node may lack metrics, but a Pending node that has become an NLB backend must require complete fresh metrics.
- An early post-expansion protection/deregister/scale/approval attempt must not reach Infra or Raptor before the persisted readiness witness.
- A stale or mismatched four-node observation must not unlock writes, including after an agent resume.
- The 420-second hold can expire before bootstrap completes; the rehearsal must mark this INCONCLUSIVE/FAIL and recover rather than claim PASS.

---

### Task 1: Terraform scale-out hold

**Files:** Modify `terraform-module/pgcat-ess/main.tf`; test `terraform-module/pgcat-ess/tests/contract.tftest.hcl`.

**Interfaces:** One `alicloud_ess_lifecycle_hook` in the PgCat module bound to `alicloud_ess_scaling_group.proxy.id`, with `lifecycle_transition="SCALE_OUT"`, `heartbeat_timeout=420`, `default_result="CONTINUE"`. No environment-root IAM or new NLB attachment.

- [ ] Add a failing Terraform plan assertion for the exact hook scope/transition/timeout/default and unchanged desired two/min two/max four.
- [ ] Run the module test; confirm that assertion fails for a missing hook.
- [ ] Add the hook in the PgCat module and run `terraform fmt -check -recursive`, module `terraform test` and environment validate through the pinned Terraform runtime or CI. Require green output.
- [ ] Commit the Terraform change and test alone. Do not apply from the Mac.

### Task 2: Transition-aware read observations

**Files:** Modify `components/agent-harness/observation-tools.mjs`, `observation-extension.mjs` if its return description needs clarity; test `components/agent-harness/observation-tools.test.mjs` and `pgcat-metrics.test.mjs` only if the shared metric parser changes.

**Interfaces:** `cloudRead({kind:'backend'})` retains exact group/identity checks and returns `status` plus `healthy:false` for documented transitional backend states. `metricsRead({kind:'pgcatClients'})` returns validated current-backend nodes and explicit `pendingInstanceIds` for Pending/Pending:Wait members absent from NLB; it requires complete metrics for every backend and other member. `dbConnectionProbe({instanceId})` returns `{instanceId,connected:false,readiness:'pending'}` only for a known Pending or unhealthy/unregistered node, and still performs read-only SQL for a healthy registered one.

- [ ] Write failing fixtures for three documented NLB transition statuses, unknown status, foreign backend, Pending absent NLB without a scrape, Pending registered without a scrape, a missing InService scrape, known unready SQL target, and healthy-node SQL failure.
- [ ] Run the focused Node tests and observe the expected failures.
- [ ] Implement the minimum read-path change. Do not catch malformed/authentication/timeouts as pending readiness; only validated provider lifecycle/health may produce pending data.
- [ ] Run focused tests, the agent-harness suite and diff check; commit this task alone.

### Task 3: Post-expansion read/write gate

**Files:** Create `components/agent-harness/replacement-readiness.mjs` and `replacement-readiness.test.mjs`; modify `components/agent-harness/live-replacement.mjs`, `replacement-read-queue.test.mjs` and, if required for the witness record, `replacement-trace.mjs` plus its tests.

**Interfaces:** `ReplacementReadiness` consumes the persisted replacement trace plus successful fresh `cloudRead` fleet/backend/capacity, `metricsRead pgcatClients`, and two new-node `dbConnectionProbe` results. It records a request/definition/skill/model/scope-bound `readyFour` witness once all four exact healthy backends, complete metrics, desired four and two successful new-node probes align within 30 seconds. After an accepted 2→4 action and before that witness, active tools expose reads/waits only; `beforeTool` also rejects every post-expansion write or approval reaching MCP in the same round. The witness remains valid across resource waits and human approval resume; later 4→3/3→2 safety checks remain with existing Infra gates.

- [ ] Add failing tests for accepted expansion relocking, early attempted protection/deregister/approval with zero MCP writes, fresh exact four-node unlock, stale/missing/foreign evidence rejection, and witness preservation on resume.
- [ ] Run the focused tests RED, implement the smallest witness and active-tool transitions, then run focused and full agent-harness tests GREEN.
- [ ] Keep the read-only qualification path and initial two-node baseline behavior unchanged; commit the gate and tests alone.

### Task 4: Rehearsal and release procedure

**Files:** Modify `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md`; add exact rehearsal fixtures to `test-assets/traffic/test_provider_feed.py` or its current test owner only if an existing fixture does not prove Pending target publication.

**Interfaces:** The runbook adds a bounded disabled-mode 2→3 readiness rehearsal after Terraform hook apply. It requires one provider action, continuous feed/capture before the action, Pending target before NLB registration, fresh scrape/triplets before registration, strict assessment, and separately reviewed exact 3→2 recovery with target reconciliation. A failed rehearsal blocks the full test. Hook timeout does not imply a healthy node.

- [ ] Add the missing Pending target fixture if needed; run it RED/GREEN against existing feed behavior without weakening the assessor.
- [ ] Write the rehearsal, cost/time, timeout and recovery gates with exact CLI/profile/scope references and no secret material.
- [ ] Review the runbook against the spec, run the traffic test suite and diff check, then commit.

### Task 5: Review, GitOps and live gates

- [ ] Obtain one independent whole-change code review; address Critical/Important findings with focused failing tests and rerun affected suites.
- [ ] Push one PR against current remote main. Require Terraform PR plan comment, CI and reviewed apply of the exact hook only; verify the applied hook and unchanged two-node ESS/NLB/Gateway baseline.
- [ ] Publish reviewed AX Pi/Gateway images only if Task 2/3 changed their source. Pin immutable digests via GitOps PR, verify actual image IDs, readiness, Argo sync, false Gateway flag and idle state.
- [ ] Run a short disabled capture, then one budgeted disabled 2→3 rehearsal with retained strict evidence and exact reviewed recovery. No full replacement request until these gates are verified.
- [ ] If the rehearsal passes and time/budget permit, run one new full 2→4→3→2 request under the existing human approval and continuous-capture runbook. Otherwise hand off the concrete gate and state by 22:30 Shanghai.
