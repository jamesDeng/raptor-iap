# PgCat No-Scale Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce one authenticated replacement-disabled Mac-lane timing PASS or an honest FAIL/INCONCLUSIVE, with exact finite capture and process cleanup.

**Architecture:** A 180-second maximum provider feed and 65-second worker collector remain under one Mac lane owner. A worker-local assessor seals the retained evidence; the Mac controller consumes that verdict through the owned lane, stops its exact feed gracefully and binds a final no-scale proof to the current target, source and clients. The ordinary transition assessment remains separate.

**Tech Stack:** Python 3.6-compatible worker code, local Mac Python 3.9, existing unittest fixtures, GitOps source pinning.

**Spec:** `docs/superpowers/specs/2026-10-27-pgcat-no-scale-closure-design.md`

## Global Constraints

- Existing checkout only; preserve unrelated iCloud `* 2` files and private credentials.
- No cloud resource provisioning, ESS/NLB/Gateway mutation, Raptor request or client finish in no-scale mode.
- Provider interval remains 15 seconds; loop maximum is 180 seconds; collector duration is 65 seconds and authenticated coverage is at least 60 seconds.
- Require 120 seconds startup, 180 seconds feed and 60 seconds cleanup before the 22:30 Shanghai stop, so no-scale admission closes at 22:24.
- Every Workbench action stays under the same child-owning supervisor and requires a later successful same-session provider publication.
- Observed adverse SQL is FAIL; uncertain/missing evidence is INCONCLUSIVE; `finished` never changes the verdict.
- No live proof until independent source review, exact remote staged-byte verification and fresh replacement-disabled preflight. Paid rehearsal and full replacement remain blocked until separate PASS gates.

## Review Focus

- Collector exits just before/after a status call: Task 2 tests exact identity, completion and one-use status chronology.
- Repeated inventory sequence between feed ticks: Task 3 tests repeats allowed but skipped distinct sequence refused.
- Adverse SQL in a partially corrupt segment: Task 3 tests FAIL precedence after authenticating later segments.
- Stop intent arrives during a provider tick: Task 4 tests at most one further publication and exact clean exit.
- Paid run uses a new private binding and the supervisor exits after durable `finished`: Task 5 tests stable target/source/client hashes across fresh paths and exact sealed final-state reconciliation without inferring success from exit.

---

### Task 1: Retain only the identity fields this proof needs

**Files:** Modify `test-assets/traffic/worker_capture.py`, `test-assets/traffic/capture_window.py`; test `test-assets/traffic/test_worker_capture.py`, `test-assets/traffic/test_capture_window.py`, and the valid Kubernetes fixtures in `test-assets/traffic/test_transition_integration.py`.

**Interfaces:** `capture_frame(...) -> dict` adds `deployment:{uid,generation,observedGeneration,desired,ready}` and each Pod's exact `name,containerId,ready`. `clean(frame) -> dict` preserves those bounded validated fields only for schema-2 frames; historical absent fields stay readable but cannot satisfy Task 3.

- [ ] Add focused failing tests for valid identity retention, malformed/partial field refusal, no Pod IP retention, and historical schema compatibility; update the transition integration fixture with real valid Deployment metadata/status and Pod Ready fields so its complete-source PASS still tests a complete capture.
- [ ] Run the focused tests and record RED failures.
- [ ] Implement the minimal capture and sanitizer changes with fixed field names and type/length bounds.
- [ ] Run focused normal and `python3 -O` tests, then the full traffic suite; require all PASS.
- [ ] Commit only these five files and obtain independent scoped review before Task 2.

### Task 2: Finite worker collector with sealed exit

**Files:** Create `test-assets/traffic/pgcat_collector_control.py`; test `test-assets/traffic/test_pgcat_collector_control.py`; modify fixed `collector-launch/status` commands and parsers in `test-assets/traffic/pgcat_lane_actions.py`, payload validation in `test-assets/traffic/pgcat_lane_protocol.py`, source-hash checks in `test-assets/traffic/pgcat_lane_supervisor.py` and `test-assets/traffic/pgcat_rehearsal_controller.py`, and their matching fixture tests.

**Interfaces:** A fresh private run-root binding supplies `run`, `contract`, `kubeconfig`, exact source hashes including the new `collectorControl` key for `run/pgcat_collector_control.py`. The fixed worker entry verifies and executes exact staged source bytes, including transitive worker modules, so stale `.pyc` cannot bypass the reviewed source. `launch` starts one detached 65-second `worker_capture.py` without `--boundary-dir`; `status` returns only `ALIVE` with exact process identity or `EXITED` with the same identity and `{category:'completed',exitCode:0,completionSha256}` after sealed capture completion. The worker accepts no arbitrary command/path from a request.

- [ ] Add failing fake-child tests for one launch, 65-second argv, no boundary, ALIVE→EXITED, exact identity, valid completion hash, duplicate launch, missing completion, nonzero exit and timeout. A matching-metadata stale `.pyc` fixture must execute the reviewed `.py` bytes, never cached code.
- [ ] Run RED tests, implement the fixed worker control and result parser, then run GREEN in normal/optimized modes and Python 3.6 grammar checks.
- [ ] Run the full traffic suite and commit scoped files; obtain independent review before Task 3.

### Task 3: Worker-local lane-timing verdict

**Files:** Create `test-assets/traffic/pgcat_lane_timing_assessor.py`; test `test-assets/traffic/test_pgcat_lane_timing_assessor.py`; extend `test-assets/traffic/capture_window.py` and its tests with a narrow opt-out for transition completion only, and extend worker result parsing and one-use verdict journal in `pgcat_lane_actions.py` and `pgcat_lane_protocol.py` with focused tests.

**Interfaces:** `assess_lane_timing(capture_dir, collector_identity, target, run_context, source_hashes) -> dict` independently verifies `completion.json` and every listed segment SHA, calls existing `capture_window.assess(..., require_transition=False)` for the shared SQL/event/continuity rules, then applies the stricter lane-only roster/identity/metrics checks. The new `require_transition` argument defaults to `True`, preserving ordinary behavior; ordinary `assess_manifest` remains untouched. Fixed `lane-verdict` action passes only the bound private paths. The address-free verdict binds run/feed session, target/source, collector identity and capture digests.

- [ ] Add failing fixtures for authentic 65-second/≥60-second coverage and each rule in the spec: ≤3-second gaps, stable two Ready Pod/container/process and Deployment identities, exact original ESS/NLB roster, repeated versus skipped distinct feed sequence, <30-second provider age, ≤45-second complete per-node metrics, advancing clean SQL, and FAIL precedence on authenticated adverse SQL. Prove default `capture_window.assess` still requires a transition while only the new lane call may skip that one completion rule.
- [ ] Run RED fixtures, implement minimal assessor and sealed worker verdict, then run GREEN normal/optimized and Python 3.6 grammar checks.
- [ ] Test that caller text or an unrelated verdict hash cannot enter the journal; run full traffic suite, commit and obtain independent review before Task 4.

### Task 4: Exact graceful feed stop and final freshness

**Files:** Modify `test-assets/traffic/provider_feed.py`, `test-assets/traffic/pgcat_rehearsal_controller.py`, `test-assets/traffic/pgcat_lane_supervisor.py`, `test-assets/traffic/pgcat_lane_protocol.py`; tests in their matching `test_*.py` files.

**Interfaces:** The feed checks no-scale exact-two target on every read and a private one-use stop intent after publication/before next read. `Controller.stop_no_scale(verdict_result) -> dict` writes the fsynced intent only after `EXITED`, a sealed verdict and later provider publication, then waits on its exact `Popen` for exit zero and reads the final same-session receipt. An intent during a tick permits at most one more exact-two publication. Natural 180-second timeout alone cannot PASS.

- [ ] Add failing fake-clock/feed tests for 22:24 admission, unchanged 15-second interval, exact-two-only publication, one-use stop, in-tick race, no acknowledgement, natural timeout and final receipt age ≥30 seconds.
- [ ] Run RED, implement stop state and controller chronology, then run GREEN normal/optimized and full traffic suite.
- [ ] Commit scoped files and obtain independent review before Task 5.

### Task 5: Bind final no-scale proof to the paid preflight

**Files:** Modify `test-assets/traffic/pgcat_rehearsal_controller.py`, `test-assets/traffic/pgcat_lane_supervisor.py`, `test-assets/traffic/pgcat_lane_protocol.py`, `test-assets/traffic/pgcat_lane_client.py`; matching tests and the active `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md`.

**Interfaces:** `finish_no_scale(...)` accepts only a deterministic assessment constructed from the journaled worker verdict, exact feed stop/exit and final receipt, and persists a private proof with date, evidence SHA, stable target/source/client hashes and original binding hash. The paid `preflight.json` extends its `noScaleProof` object to `{status,evidenceSha256,proofPath}`; `proofPath` is an absolute mode-0600 record in a mode-0700 prior-run directory, outside the fresh paid capture root. `_paid_evidence(...)` opens that exact no-symlink file, verifies its bytes/digest and stable hashes, and separately verifies the paid run's own new binding.

- [ ] Add failing tests for forged caller PASS, wrong run/session/collector/target/source/client, stale day, changed private proof, changed paid binding, observed SQL FAIL and incomplete exact process/credential cleanup before PASS. Reproduce the inherited finish race with a deterministic stale-active-snapshot delay; require exact sealed journal reconciliation after supervisor exit and refuse an unsealed exit.
- [ ] Run RED, implement binding/verification and explicit runbook amendment from 75-second blocked path to reviewed 180-second maximum.
- [ ] Run GREEN normal/optimized, full traffic suite, Python 3.6 guards and diff checks; commit scoped files, then obtain independent review.

### Task 6: Whole-chain review and disabled release gate

**Files:** Update private source manifest and `working/handoffs/2026-10-27-pgcat-live.md` only after code review; retain evidence outside Git.

**Interfaces:** Exact source-hash/staged-byte comparison includes collector, assessor, feed and all transitive worker modules. The old no-scale/scratch source cannot launch. A fresh disabled proof may run only when all Task 1–5 reviews pass, live account/fleet/Gateway/client/metric checks are fresh, the same-day CNY ceiling observation is reused, and the 22:24 start bound is satisfied.

- [ ] Review the whole branch against this plan and resolve blocking findings.
- [ ] Before any ACK/Workbench command read `working/skills/ack-workbench-cli/SKILL.md`. Verify all traffic tests, optimized tests, Python 3.6 worker syntax, source hashes and exact staged bytes. Do not touch credential content.
- [ ] Push/merge reviewed source under the user's standing authorization, then perform one finite disabled no-scale proof and independently assess retained evidence/cleanup. If any check refuses, record INCONCLUSIVE and do not start paid rehearsal.
