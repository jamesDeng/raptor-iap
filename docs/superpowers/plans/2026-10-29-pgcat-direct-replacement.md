# PgCat Direct Replacement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Implement and review each task sequentially without repeated owner approval.

**Goal:** Implement the owner's 2→4→2 replacement sequence with app rolling restart, actual zero-client gates, versioned native result and compatible retained evidence.
**Architecture:** Infra independently protects serving capacity; the request-bound harness uses its immutable baseline/ready-four cohort for lineage and ordering. Existing provider serialization, metrics, approval claims and no-replay rules remain. New direct evidence has explicit semantics; historical staged evidence remains readable.
**Spec:** `docs/superpowers/specs/2026-10-29-pgcat-direct-replacement-design.md`.
**Tech:** Go policy/Gateway, Node harness, Python provider/capture, Go test client, existing Helm/GitOps release.

## Global Constraints

- Existing linked checkout/branch only. Preserve unrelated iCloud/untracked files. No cloud, credential, deployment, billing, push or merge from implementers.
- Keep replacement disabled and public paid release structurally closed. This plan alone does not enable the incomplete full observer/closure path.
- No global weakening of majority policy, model-supplied baseline, generic command bypass, approval bypass, automatic unknown-action retry, or new database service.
- Originals come from persisted request baseline; replacements come from persisted readyFour evidence. Final survivors must equal that replacement pair.
- Both originals must be withdrawn, app rollout observed, and every pool/user active/idle/waiting zero on both before direct 4→2. Fresh evidence is required after pause.
- Version2 means exactly [[2,4],[4,2]]. Historical version1 means exactly [[2,4],[4,3],[3,2]]. Acceptance stays pending.
- Never move skills-v0.4.0. New direct instructions require a verified new immutable release and fresh request; no silent in-flight migration.
- Keep old full-replacement/absent-mode and disabled-rehearsal semantics. New mode is full-replacement-direct-v2. Failure precedence and freshness/gap limits unchanged.
- Sequential source editors/test suites. Tests are source evidence, never a live PASS. Budget1800 net, once/day live-preflight billing, paid cutoff21:30, POC stop22:30 Shanghai.

## Review Focus

- Equal-half exception with incomplete/unhealthy/unprotected capacity, foreign/duplicate removal IDs or changed second snapshot (Task1).
- Model chooses a replacement as an original, skips withdrawal/restart, uses stale zeros after pause, or substitutes another final cohort (Task2).
- Version/tag/shape mismatch upgrades historical staged execution or relabels its result (Task2).
- Desired4→2 followed by delayed physical departures permits a third ID/lost replacement, or loses old metric obligations (Task3).
- SIGTERM during admitted queue work skips SQL, loses final counters or exceeds the Pod grace budget (Task4).

### Task 1: Narrow protected-capacity policy and direct scale-in guard

**Files:** Modify `components/infra-api/internal/policy/proxy.go`, `internal/commands/runtime.go`; corresponding policy/runtime/fleet tests and `cmd/server/proxy_startup_test.go` only as necessary. Add a focused `internal/commands/direct_replacement_test.go` if useful. No provider API/auth schema changes.
**Interfaces:** Keep `DeregistrationAllowed(s, backend, ids)` signature. Its existing majority success stays; otherwise allow only desired4/min2/max4/exact4 unique members with >=2 remaining protected+healthy+registered members excluding all requested IDs. Add `DirectReplacementScaleInAllowed(s ProxySnapshot, desired int) error`: no-op outside exact desired4/min2/max4→2; inside require exact4 unique members, exactly2 protected healthy registered and exactly2 unprotected withdrawn members. Invoke before claim/submission, alongside existing zero metrics and unchanged checks. No fabricated lineage claim in server.

- [ ] Write failing policy cases: batch-two and second sequential removal accepted only with complete protected healthy retained pair; before-expansion/one survivor/unprotected/unhealthy/unregistered/wrong bounds/foreign/duplicate rejected. Generic majority fixtures unchanged.
- [ ] Write failing runtime cases for direct4→2: only both drained unprotected+both healthy protected retained accepted; connected/missing/stale metrics, extra eligible node, protected old/registered eligible, missing approval/claim, changed snapshot and unknown ack remain blocked/non-replayed.
- [ ] Run focused RED tests, implement bounded checks, run focused GREEN then full `go test ./...` and `go vet ./...` in components/infra-api once on frozen code. Retain exact output.
- [ ] Self-review and commit only owned files. Report to task-1-report.md; parent supplies independent task review.

### Task 2: Versioned native direct sequence and immutable skill contract

**Files:** Harness `replacement-trace.mjs`, `replacement-readiness.mjs`, `replacement-completion.mjs`, `live-replacement.mjs`, `pinned-replacement-skill.mjs`, `observation-tools.mjs`; create `replacement-sequence.mjs` and matching tests. Modify corresponding harness fixtures/helpers only where the contract changes. Gateway `internal/execution/replacement_result.go`, `Dockerfile`, `internal/runtime/live.go` (fixed harnessFiles staging manifest), matching result/runtime/journal/package tests, and `scripts/rdev/test_qualification_packaging.py` only if needed for module shipment coverage. Skill `agent-skills/pgcat-replacement/{SKILL.md,references/contracts.md,references/recovery.md}`. No Python lane/client source or release pin changes.
**Interfaces:** New `ReplacementSequence(manager,trace,readiness,profile)` provides `observe(kind,data,args)`, `beforeMutation(tool,args)`, `onMutationResult(tool,args,receipt)`, `cohort()`, `completionEvidence()`. Profile `direct-v2` must derive from an exact verified skill-content marker `replacement-contract: direct-v2`, through new `pinnedReplacementProfile(skills)` export; unsupported/missing marker refuses direct execution before mutation. Existing instruction loader remains bounded and symlink-safe. Historical stored results are readable; old selected skills are not silently upgraded.

Use existing protected session custom entries for minimal allowlisted ordering/generation evidence tied to trace identity. Preserve actual initial exact2 fleet/backend/capacity, complete metrics/SQL and bound Deployment checks before initial writes. readyFour defines the only new pair. Permit protection only of baseline originals or readyFour pair as appropriate, original-only deregistration, restart only after both backend absences and healthy protected replacements, original-only unprotection after observed rollout and fresh zero metrics, and one 4→2 action after rechecking those gates. Allow one batch or two disjoint original withdrawals; submitted/unknown actions are never replayed. No desired3 direct mutation. Exact scope checks/approval/pause remain.

Hook observations from existing trusted cloud/metrics/Deployment reads and mutation receipts; no model booleans. The current observation-tools Deployment return omits generation: add validated generation and observedGeneration from actual Kubernetes metadata/status, with matching observation tests. Validate raw complete pool triplets, freshness and generation (fresh Deployment observedGeneration>=restart receipt generation, correct UID, ready/updated/available2). Reconstruct minimal persisted state on resume, require fresh current evidence, and retain original trace replay protections. `cohort()` returns exactly `{oldInstanceIds,newInstanceIds}`; `completionEvidence()` adds the actual recorded `restartGeneration` to those fields after validating order/current evidence. `qualifyReplacementConvergence` consumes the cohort/rollout evidence, emits version2 with exactly2 action stages and exact new pair. Gateway validation dispatches exact stages by version (v1=legacy, v2=direct), rejecting mixed action lists, duplicate IDs/actions and PASS acceptance. Instruct bounded resource_wait after restart until rollout is observed; do not query strict two-Ready-client traffic metrics during a surge and mistake that temporary discovery refusal for successful traffic.

- [ ] Add RED tests for every Review Focus item, initial writes after fleet-only, premature restart/one old still registered, wrong new protection pair, nonzero/incomplete/stale pools, wrong app UID/generation, direct desired3, replay/unknown, resume fresh evidence, replaced cohort and absent profile. Include a real harness-produced v2 JSON fixture validated by Gateway plus v1 stored-result compatibility.
- [ ] Implement the smallest composed sequence gate; update skill to one expansion, both-old withdrawal, bound app restart, both-zero gate, one exact4→2 approval and convergence. V2 exports required positive safe-integer restartGeneration from the actual receipt, validated by Gateway; v1 stays historical without a fabricated generation. Ship the new module in both image COPY and harnessFiles. Keep new release candidate skills-v0.5.0 documented, do not create/move tags here.
- [ ] Run focused RED/GREEN; freeze source; run harness `node --test *.test.mjs`, Gateway `go test ./...` and `go vet ./...` once, reporting actual environment/integration limits. No synthetic claim of DB-backed tests if unavailable.
- [ ] Self-review/commit owned paths, report task-2-report.md, then parent independent review.

### Task 3: Explicit direct provider/capture transition mode

**Files:** `test-assets/traffic/provider_feed.py`, `capture_window.py`, corresponding provider/capture/rehearsal/transition tests, `README.md`. Add `test_direct_replacement_mode.py` for cohesive end-to-end fixtures. No local lane/worker/collector admission enablement.
**Interfaces:** Add mode `full-replacement-direct-v2` to existing CLI, emitted inventory and inventory sanitizer. `InventoryMode` binds initial exact2 originals, observed expansion4 and exact2 added IDs, then one desired4→2; supports temporary3/4 physical members while desired2. Final exact membership/backend healthy set equals added pair, originals absent. `Operator.departures_for` and TransitionEvidence grant2 departures once for that direct drop, restricted to originals, not2 independent actions. Historical modes unchanged.

- [ ] RED fixtures cover full2→4→2 including asynchronous physical removal, two old zero/absence retirements and continuous fresh evidence. Reject desired3, skipped expansion, wrong/extra new ID, lost replacement, early/unexplained original departure, reappearance, mode/session/sequence change, stale/missing metrics and incomplete final membership.
- [ ] Implement producer/consumer consistently; retain targets and old node obligations until their existing complete-zero-plus-later-absence proof. No receipt/clock/deadline widening.
- [ ] Focused RED/GREEN, Python3.6 grammar checks, then full traffic suite normal/optimized once each on frozen code. Report any existing failures honestly; no retry-to-green.
- [ ] Self-review/commit and task-3-report.md; parent independent review.

### Task 4: Graceful application rolling restart

**Files:** `components/test-client/cmd/client/main.go`, existing finish tests, `internal/traffic/runner.go`/tests only if required; Helm client Deployment/values and `tests/test_test_env_charts.py` only for measured grace change; traffic README. No new session-rotation socket/API.
**Interfaces:** SIGTERM stops admitting new schedules while already admitted active/queued SQL drains under existing5s per-op deadlines; close all active/idle sessions; emit and hold final evidence. Preserve private end-of-run finish behavior, exact identities and two-minute hold. Existing rolling strategy maxUnavailable0/maxSurge1 stays. Compute bounded drain+10s SIGTERM final hold+2s HTTP shutdown and make chart grace cover it with explicit margin if current20s insufficient; do not widen SQL deadlines or suppress failures.

- [ ] RED test actual signal/production shutdown coordination with in-flight plus queued operation: all admitted operations get terminal outcomes, no shutdown-induced skipped, sessions close, final counters readable before exit, bounded deadline. Test timeout/failure remains visible and private finish still works.
- [ ] Implement minimal separation of scheduling stop from accepted-work cancellation; no alternate client or evidence reset. Exercise a rollout-shaped capture fixture retaining old finals and successor startup/counter evidence through surge, with unchanged failure precedence.
- [ ] Focused RED/GREEN, full client `go test -race ./...`/`go vet ./...`, existing chart render tests and affected traffic fixtures once frozen. Preserve real integration checks for CI if local DB/container runtime unavailable.
- [ ] Self-review/commit and task-4-report.md; parent independent review.

## Integration and continuation

After task reviews, perform whole-branch review and required CI before merge/release.
Existing PR304 remains draft while its saved serve-loop/closure work is incomplete;
do not publish a claimed live-ready direct contract by merely merging source.
Then implement native observer closure reusing existing guardian/whole-window
verdict/restoration/credential gates without redispatching completed units or
finishing candidate-only no-restart closure before duplicating it for native.
Native rollout evidence must bind all predecessor finals and the terminal
successor pair; old originalRoster-only finisher cannot be relabeled. The old
paid2→3→2 rehearsal is not a prerequisite for the owner-selected new strategy;
the disabled no-scale native startup/restart/closure proof is mandatory instead.
Only a final reviewed source graph and that actual proof can unlock the live
direct test. Re-discover fresh fleet/scope, perform one daily cost check, deploy immutable
images through GitOps, verify loaded flag remains false, release new immutable
skill tag and use a fresh request. Record actual findings and stop rather than
automatically reversing an uncertain cloud action. No source test is native or
full replacement PASS.
