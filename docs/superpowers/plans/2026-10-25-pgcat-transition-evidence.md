# PgCat Transition Evidence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retain complete, assessable provider and traffic evidence while PgCat's two-node replacement passes through 2→4→3→2, without weakening any PASS gate.

**Architecture:** The local provider feed publishes schema-2 ESS and NLB rosters including transition states and maintains monitoring targets for every backend seen during the window. The worker collector retains only allowlisted state, while the assessor requires fresh metrics for every current or not-yet-proven-drained backend and keeps all existing SQL, roster, poll-gap and finalization rules. Version-1 archives retain their original interpretation.

**Tech Stack:** Python standard library, unittest, existing Alibaba CLI and Workbench CLI adapters; worker Python 3.6 compatibility.

**Spec:** `docs/superpowers/specs/2026-10-25-pgcat-transition-evidence-design.md`

## Global Constraints

- No new cloud resource, IAM grant, credential output, raw provider payload in evidence, or direct cloud provisioning. Preserve Terraform/GitOps ownership.
- Keep the provider refresh deadline at 15 seconds, the provider age limit at 30 seconds, the metrics observed age limit at 3 seconds, and the client poll gap limit at 3 seconds.
- NLB backend presence requires complete metrics irrespective of its status, weight, or health; old-node obligation ends only after fresh all-pool/user zero triplets and subsequent backend absence.
- Keep SQL Failure/Timeout/Ambiguous as FAIL and missing/uncertain provider, metrics, lifecycle, client, or final marker evidence as INCONCLUSIVE.
- Preserve historical version-1 assessment; never upgrade the 2026-10-25 stopped attempt.
- Preserve the uncommitted runbook edit and iCloud ` 2` duplicate files. Commit only explicitly listed task files.

## Review Focus

- New `Adding` backend appears before exporter scrape: a schema-2 frame is INCONCLUSIVE, never PASS (Task 2 test).
- Old backend becomes zero weight or disappears before fresh all-pool/user zero: its missing metric is INCONCLUSIVE (Task 2 test).
- All NLB backends unhealthy: best-effort forwarding makes the frame INCONCLUSIVE (Task 2 test).
- First client frame has a Failure/Timeout/Ambiguous/Skipped counter: no first-frame baseline hides it (Task 2 test).
- A provider feed restarts or loses a target during a transition: source and retained evidence do not silently present a complete window (Tasks 1 and 2 tests).

---

### Task 1: Complete transition-aware provider snapshot

**Files:**
- Modify: `test-assets/traffic/provider_feed.py`
- Test: `test-assets/traffic/test_provider_feed.py`
- Test: `test-assets/traffic/test_worker_python36.py` if the same-script patch/readback changes its command expectation
- Update: `test-assets/traffic/README.md`

**Interfaces:**
- `snapshot(fetch_ess, fetch_group, fetch_ecs, fetch_nlb, *, group_id, server_group_id, now, known_ids=(), allowed_departures=0, required_address_ids=()) -> dict` returns schema-2 provider fields plus transient `privateAddresses`. `allowed_departures` counts only observed sequential 4→3 and 3→2 desired decreases; an NLB-only ID must be in `known_ids`, and previously seen ESS IDs absent now may not exceed that count. `required_address_ids` remembers any node previously address-known, NLB-registered, or service-ready. `Operator` adds its own immutable `feedSession` UUID and next successful-publication `sequence` before publication.
- `collect_transition_members(fetch_ess, group_id) -> list[dict]` validates complete pagination and preserves permitted lifecycle/health states without changing `test-assets/metrics/provider_inventory.py`.
- `Operator` retains exact earlier ESS IDs, last desired capacity, sequential decrease count, required-address IDs, targets, one UUID `feedSession`, and successful-publication sequence for the single process lifetime. `publish(..., retain_targets=())` writes only the schema-2 allowlist to the worker after resourceVersion-checked target reconciliation.

- [ ] **Step 1: Write failing tests** for Pending ESS with no ECS address and no NLB backend, `Adding`/`Configuring`/`Removing` NLB backends, NLB-only old backend after desired decrease but delayed ESS removal, unexpected removal without decrease, foreign backend, missing ECS address for any backend, changed pagination, retained target lifetime, and feed UUID/sequence. Assert exact schema-2 fields and no address in published evidence.
- [ ] **Step 2: Run** `python3 -m unittest discover -s test-assets/traffic -p test_provider_feed.py -q`; confirm expected failures in the new cases.
- [ ] **Step 3: Implement** the interfaces above. Keep `healthyRegistered` equal to current ESS IDs with Available/positive-weight/Normal NLB health. Reject unknown states and unverified foreign backends; do not renew observedAt from stale data. Keep fixed-category error output from PR #295.
- [ ] **Step 4: Run** the provider test file and the metrics inventory tests; confirm both green and `git diff --check` clean.
- [ ] **Step 5: Commit** only the Task 1 files listed above with a focused message.

### Task 2: Retain and assess schema-2 frames

**Files:**
- Modify: `test-assets/traffic/worker_capture.py`
- Modify: `test-assets/traffic/capture_window.py`
- Test: `test-assets/traffic/test_worker_capture.py`
- Test: `test-assets/traffic/test_capture_window.py`
- Test: `test-assets/traffic/test_segmented_capture.py`

**Interfaces:**
- `capture_frame()` copies the exact schema-2 provider allowlist into each frame; an absent or malformed field yields retained unavailable evidence, never a fabricated healthy default.
- `clean(frame) -> dict` preserves schema 1 when `schemaVersion` is absent and schema 2 only when all exact fields and row keys are present. `assess` rejects a changed `feedSession` or nonmonotonic `sequence`.
- `assess(frames, ...) -> dict` maintains a per-ID backend obligation across frames. It releases an old ID only after fresh complete all-pool/user zero triplets followed by a fresh provider frame proving backend absence. Its existing SQL and finalization rules remain authoritative.

- [ ] **Step 1: Write failing fixtures** for all five Review Focus cases, schema-2 complete 2→4→3→2 transitions, missing/duplicate/foreign state rows, stale provider/metrics, and historical schema-1 unchanged. Explicitly test first-frame negative counters and SQL-failure precedence.
- [ ] **Step 2: Run** the worker, capture and segmented tests; confirm the new cases fail for the expected missing behavior.
- [ ] **Step 3: Implement** schema-2 capture and assessment using the Task 1 interface; keep schema-1 behavior byte-for-byte compatible where possible. Do not infer non-serving from NLB transition status.
- [ ] **Step 4: Run** `python3 -m unittest discover -s test-assets/traffic -p 'test_*.py' -q` and `python3 -m unittest discover -s test-assets/metrics -p 'test_*.py' -q`; confirm both green and `git diff --check` clean.
- [ ] **Step 5: Commit** only the five Task 2 files.

### Task 3: End-to-end local proof and release gate

**Files:**
- Modify: `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md`
- Create: `test-assets/traffic/test_transition_integration.py`
- Test: `test-assets/traffic/test_worker_python36.py`

**Interfaces:**
- A synthetic full 2→4→3→2 manifest assesses traffic PASS only with all provider/metric and SQL conditions; approval receipts remain a separate overall acceptance gate. Missing traffic evidence assesses INCONCLUSIVE or FAIL as defined in the spec.
- The runbook's next live gate references the reviewed source commit and refuses a paid attempt after 21:30 Shanghai or without full cleanup capacity by 22:30.

- [ ] **Step 1: Add a full version-2 synthetic transition fixture** in the new test file and run it. If it fails, retain the failing fixture and correct the responsible implementation in its owning Task 1/2 file before proceeding; do not weaken the fixture. Extend the existing Python 3.6 parse test only if the changed modules are not already covered.
- [ ] **Step 2: Update** the runbook with version-2 feed/collector hash verification, target lifetime, zero proof, final target reconciliation, and the preserved time/cost gates. Keep the current live request cancelled and `replacementEnabled=false`; do not launch a paid run as part of this task.
- [ ] **Step 3: Run** all traffic/metrics Python suites, Python 3.6 compatibility, `git diff --check`, and a whole-branch independent review.
- [ ] **Step 4: Commit** only the Task 3 test/runbook files and prepare a PR. Merge only after CI and review; use a fresh disabled-mode smoke before any later paid replacement attempt.
