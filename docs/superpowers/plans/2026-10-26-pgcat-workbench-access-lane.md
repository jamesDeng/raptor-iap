# PgCat Workbench Access Lane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent operator Workbench `exec` calls from occupying the provider feed's 15-second deadline during a disabled PgCat rehearsal.

**Architecture:** A small Mac-side wrapper serializes exact Workbench CLI processes with one owner-only lock. External calls consume one fresh same-session receipt and a five-second lease; long worker phases run detached, while final reconciliation stays a single short action-time call. Local feed and collector supervisors retain sanitized failure receipts.

**Tech Stack:** Python 3 standard library (`fcntl`, `subprocess`, `signal`, `unittest`), existing Workbench CLI and provider feed; worker scripts remain Python 3.6 compatible.

**Spec:** `docs/superpowers/specs/2026-10-26-pgcat-workbench-access-lane-design.md`

## Global Constraints

- Keep the existing checkout and iCloud `* 2` files untouched; no new cloud resource, role or GitOps workload.
- Keep `replacementEnabled=false` and perform no ESS/NLB/client/Raptor mutation in the no-scale proof.
- An uncertain Workbench session, timeout, stale receipt, changed identity or interrupted feed is INCONCLUSIVE, with no automatic retry.
- Never print credentials, raw Workbench stderr, private addresses or unsanitized collector output in receipts or handoffs.
- One 15-second provider deadline, external receipt mtime under one second, observation age under seven seconds, CLI exit within five seconds and before `observedAt + 12` seconds.
- The 2026-10-26 paid-action cutoff is 21:30 Shanghai and work handoff is 22:30; do not query billing again today.

## Review Focus

- A stale or replaced receipt after lock acquisition must refuse before spawning Workbench: Task 1 test.
- A CLI that outlives its caller-enforced lease must consume the one-use intent and release the lock only when that exact local CLI process exits: Task 1 test.
- A provider lock wait must count against its original absolute deadline: Task 1 test.
- The provider's startup `existing_targets()` runs before the first tick deadline; it needs its own bounded 10-second cross-process deadline: Task 1 regression.
- Final reconciliation cannot be faked with no-scale transition markers: Task 2 test and Task 3 live gate.
- A collector that inherits ignored SIGINT must still stop once and retain a sealed exit result: separate collector-supervisor plan.

---

### Task 1: Shared local Workbench wrapper

**Files:** Create `test-assets/traffic/pgcat_workbench_lane.py`; create `test-assets/traffic/test_pgcat_workbench_lane.py`; modify `test-assets/traffic/provider_feed.py` only to pass its absolute per-tick deadline to the wrapper subprocess environment.

**Interfaces:** The executable wrapper receives the existing exact Workbench `exec -P infra-ops-poc -r ap-southeast-1 -i i-t4nj1bitmcz7cuoch82h -c COMMAND` argv, plus private environment fields `PGCAT_LANE_ROLE` (`provider` or `external`), `PGCAT_LANE_LOCK`, `PGCAT_LANE_REAL_CLI`, and for external calls `PGCAT_LANE_RECEIPT`, `PGCAT_LANE_SESSION`, `PGCAT_LANE_MIN_SEQUENCE`, `PGCAT_LANE_INTENT_DIR`. Provider calls receive `PGCAT_LANE_DEADLINE_MONOTONIC` expressed in host-wide `time.clock_gettime(time.CLOCK_MONOTONIC)` units, computed as that clock plus the provider's original remaining time. This Mac's Python 3.9 `time.monotonic()` value is process-relative and cannot be passed unchanged. This task accepts only the `disabled-rehearsal` receipt mode with desired two or three. After taking the lock and consuming any external intent, it `exec`s the CLI in the same PID with the lock descriptor inherited; the caller enforces the remaining deadline with `subprocess.run(timeout=...)`. The wrapper exits with a fixed category and writes no raw command output on refusal. The caller retains sanitized process output only when its contract expects an exact allowlisted result. The controller's final-receipt upload stays outside this wrapper because it occurs after verified feed exit.

- [ ] Write failing isolated subprocess tests: two processes exclude each other; provider lock wait expires at its absolute deadline; external call refuses stale mtime/observation/session/sequence; receipt replacement while waiting for lock refuses; one sequence cannot launch twice; caller timeout kills the exact local CLI PID, then releases the lock but consumes its intent and cannot retry; an ordinary fast call succeeds without exposing its command in refusal output.
- [ ] Run `PYTHONPATH=test-assets/traffic python3 -m unittest test_pgcat_workbench_lane -v`; confirm the new tests fail for the absent wrapper.
- [ ] Implement owner-only stable-inode lock and intent files, exact profile/region/worker validation, inherited-lock `exec` and fixed sanitized categories. Add the provider deadline environment in `Operator.command` without changing the existing 15-second loop or publication semantics; require the controller/helper to enforce the external lease with their subprocess timeout.
- [ ] Run the new tests and `PYTHONPATH=test-assets/traffic python3 -m unittest test_provider_feed -v`; confirm pass, then verify Python 3.6 grammar for worker-bound code and inspect the diff for credential strings.
- [ ] Commit only the wrapper, provider change and tests after independent review.

### Task 2: Rehearsal controller and detached phases

**Files:** Modify the private source `/private/tmp/pgcat-rehearsal-controller-20261026.py`, `/private/tmp/pgcat-pending-detached-local-20261026.py`, and tests `/private/tmp/test_pgcat_rehearsal_controller_20261026.py` and `/private/tmp/test_pgcat_pending_detached_20261026.py`; update `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md` with the frozen source hashes and launch order after review. These private scripts contain no credential and are not copied into Git as-is.

**Interfaces:** The controller passes one exact wrapper path and run-local lock/intent/status paths to feed and external calls. Its `baseline`, `pre_scale`, pending-ready and zero-gate phases launch exact worker-local one-use jobs; collector launch and sealed-status reads take a lease. `reconcile_and_close` remains one call returning both `PREPARED` and exact sealed receipt; it is never polled again through Workbench while the feed is active.

- [ ] Write failing controller/helper fixtures for every attached phase exceeding five seconds, a second call on the same publication, stale/replaced receipt, changed feed session, refused final reconcile and uncertain launch. Assert none can reach an ESS mutation after refusal.
- [ ] Run the private controller and pending-helper test files; confirm the new tests fail before the change.
- [ ] Route all live-feed Workbench calls through Task 1's wrapper and detach every potentially long worker phase. Keep the exact one-use ESS mutation, original roster and exit/cleanup gates already reviewed.
- [ ] Re-run both test files normally and with `python3 -O`; independently review the frozen hashes. A no-scale timing surrogate may read ConfigMap/frame/identity but must not fabricate `rehearsal_contract.reconcile` markers.

### Task 3: Disabled no-scale proof and paid-run decision

**Files:** Append exact timings and result to `working/handoffs/2026-10-26-pgcat-live.md`; update the active live-test runbook only after the observed proof. No cloud provisioning files.

**Interfaces:** Use a fresh owner-only private run root, existing Workbench CLI profile, same exact ACK worker and a temporary authorized kubeconfig only if the proof's bounded ACK reads require it. The proof performs no ESS/NLB/Gateway/client/Raptor mutation.

- [ ] Independently verify wrapper and controller hashes, single operator, local Git/remote-main state, provider baseline and Gateway false/idle before any live proof. Respect the paid-action and handoff cutoffs.
- [ ] Run one 75-second replacement-disabled provider feed through the wrapper; during it launch a detached harmless worker job and read sealed status through short fresh-receipt leases. Run a comparable read-only reconciliation timing surrogate, without manufactured transition markers. Record actual call durations and each feed publication.
- [ ] Verify normal feed exit, one continuous capture only if started, no poll gap, unchanged two protected ESS/NLB originals, Gateway false, clients and metrics healthy; delete exact temporary credentials and worker files. Label any gap or timeout INCONCLUSIVE.
- [ ] Obtain independent review of the receipts. Only if this proof passes may a fresh paid 2→3→2 rehearsal be considered. In that rehearsal the real combined reconciliation must finish within five seconds at action time or stop and perform separately reviewed exact recovery. A full replacement request remains blocked until a new rehearsal assesses PASS and cleanup is verified.
