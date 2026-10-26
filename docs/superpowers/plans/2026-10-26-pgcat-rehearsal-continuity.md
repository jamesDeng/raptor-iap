# PgCat Rehearsal Continuity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make one new disabled 2→3→2 rehearsal capable of retaining strict, continuous evidence without a disruptive client restart or Workbench status polling.

**Architecture:** Keep the existing provider and collector. Retire metrics for a formerly registered node only after fresh complete zero and a later provider observation of NLB absence; keep every current backend mandatory. Add an optional private local publication receipt to the Mac-side provider feed so final reconciliation can be observed without another Workbench call. The runbook skips client restart only when exact zero/identity/traffic gates pass. After a later local receipt, it stops and verifies feed exit, then uses one bounded Workbench call to validate exact targets, a matching later retained frame and the live roster before final finish within provider freshness.

**Tech Stack:** Python 3.6-compatible traffic modules, Python unittest, existing Workbench CLI and Alibaba provider feed.

**Spec:** `docs/superpowers/specs/2026-10-26-pgcat-rehearsal-continuity-design.md`

## Global Constraints

- No change to Gateway/AX images, Terraform resources, live flag, three-second poll limit, 30-second provider limit, 45-second scrape/raw limit or SQL failure precedence.
- Current NLB backends need complete fresh metrics regardless of health or weight. Missing/stale zero or absent later provider publication cannot retire an old obligation.
- The failed 2026-10-26 rehearsal remains INCONCLUSIVE; it is never reassessed as a new passing run.
- Do not edit the iCloud `* 2` duplicates or copy credentials into Git, logs, tests or receipts.
- Another paid rehearsal requires a separate fresh baseline, the day's existing cost observation, a continuous new capture and time for cleanup by 22:30 Shanghai.

## Review Focus

- A node that reappears in NLB after retirement must require metrics immediately: Task 1 test.
- A later nonzero raw count after retirement must restore its obligation: Task 1 test.
- A zero sample from only one pool/user or an expired sample must not retire: Task 1 test.
- A local feed receipt must not advance after an uncertain worker write or contain an address/credential: Task 2 test.
- Reconciliation and finalization must not rely on a local receipt alone: Task 3 runbook check against the binding spec. After confirmed feed exit, one bounded Workbench call must independently validate exact ConfigMap contents, a matching complete later collector frame and the live bound client roster before finish. Its exact command needs review and a no-scale timing proof before paid use.
- An external original-roster precheck does not bind `finish()`'s first read. Task 4's source prerequisite must reject replacements before request publication; the post-stop caller must supply the bound originals to the new explicit input, never rediscover its expected roster from live state.

---

### Task 1: Exact drained-node metric retirement

**Files:** Modify `test-assets/traffic/capture_window.py`; test `test-assets/traffic/test_rehearsal_mode.py` and `test-assets/traffic/test_capture_window.py`.

**Interfaces:** `TransitionEvidence.assess(inv, met, now, issue)` remains the entry point; a new internal retired-node set may exclude only zero-verified, later NLB-absent nodes from the nonpending-member requirement.

- [ ] Add a failing 2→3→2 fixture where the new node is zero, a later fresh provider publication shows backend absence, ESS still lists it, then its exporter vanishes before ESS departure. Assert PASS for otherwise clean, finalized traffic.
- [ ] Run `PYTHONPATH=test-assets/traffic python3 -m unittest test_rehearsal_mode -v`; confirm the fixture fails specifically with `missing metrics coverage`.
- [ ] Add negative fixtures for incomplete/stale/no zero, same provider publication, still registered backend, later nonzero and re-registration. Confirm they remain INCONCLUSIVE under the existing implementation where applicable.
- [ ] Implement the smallest retirement change in `TransitionEvidence`, keeping the current backend and SQL requirements intact.
- [ ] Run `PYTHONPATH=test-assets/traffic python3 -m unittest test_rehearsal_mode test_capture_window -v`; confirm all pass, then commit this task.

### Task 2: Local publication receipt

**Files:** Modify `test-assets/traffic/provider_feed.py`; test `test-assets/traffic/test_provider_feed.py`.

**Interfaces:** Optional CLI `--status-file ABSOLUTE_PATH`; after each successful worker publication, atomically write only `feedSession`, `sequence`, `mode`, `desired` and `observedAt` to a mode-0600 local file in an existing mode-0700 directory. The file must be initially absent. No receipt on worker publication failure.

- [ ] Add failing tests for exact receipt fields, mode and permissions after success; no receipt on worker write failure; refusal to overwrite a pre-existing path; no private address in bytes.
- [ ] Run `PYTHONPATH=test-assets/traffic python3 -m unittest test_provider_feed -v`; confirm the new tests fail for the missing interface.
- [ ] Add the optional CLI and write after successful `Operator.write` within the existing absolute 15-second deadline; preserve feed fail-closed behavior and read-only CLI retries only.
- [ ] Run `PYTHONPATH=test-assets/traffic python3 -m unittest test_provider_feed -v`; confirm all pass, then commit this task.

### Task 3: Rehearsal procedure and local verification

**Files:** Modify `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md` and the spec's reference if needed; test the complete traffic suite.

**Interfaces:** No new runtime code interface. The procedure requires two distinct fresh zero samples and unchanged client processes, one exact 3→2 action, resourceVersion-bound final ConfigMap reconciliation/readback and a later local receipt. Intentionally stop and verify feed exit; then one bounded Workbench call reads the exact ConfigMap, a complete later collector JSONL frame matching the final receipt and the live bound client roster before exact finish. Validation, both client finals and settling must complete while the last provider observation is under 30 seconds old. The exact action-time validator command still needs review and no-scale timing proof.

- [ ] Replace the unconditional in-window client restart with the zero-confirmed no-restart path and a fail-closed outside-window recovery branch for nonzero/changed clients.
- [ ] Document the local receipt as liveness only, with no Workbench status polling after final reconciliation while the feed is active. After confirmed feed exit, require one bounded Workbench validation of exact ConfigMap contents, a complete later collector frame and unchanged clean live clients before finish; fail closed if any check or the remaining 30-second budget is uncertain.
- [ ] Run `PYTHONPATH=test-assets/traffic python3 -m unittest discover -s test-assets/traffic -p 'test_*.py' -v` and `git diff --check`; confirm no failures, then commit.

### Task 4: Review, merge and proof gates

**Concrete prerequisite found during validator preparation:** the old `finish()` selected its roster from a new live read, so stable replacement clients could be selected after the caller's original-roster precheck. The private blocked report/reproducer records this offline finding; no live substitution or validator success is claimed.

**Files:** Modify `test-assets/traffic/finish_clients.py`, test `test-assets/traffic/test_finish_clients.py`, and update this plan, the binding spec and active runbook. No validator implementation or cloud action in this prerequisite.

**Interface:** Optional Python 3.6-compatible `expected_roster` on `finish()` and optional CLI `--expected-roster-file` (private regular nonsymlink mode-0600 JSON). Omission preserves legacy behavior; an explicitly supplied invalid roster, including null, refuses. The new post-stop caller must always supply the validated original roster from its private contract. Require exactly two four-field identities, canonicalize and snapshot them, compare the first live roster before `request.json`, retain the second pre-action check and exact in-Pod UID/process arguments, and never retry uncertainty.

- [x] Write and observe failing in-repository tests for first-read replacement before request, second-read mismatch before dispatch, malformed/duplicate binding, unchanged originals and private CLI file validation.
- [x] Implement the smallest bound-roster extension; preserve legacy callers and no-retry/unknown-response behavior. Verify focused/full traffic tests, Python 3.6 grammar and diff checks before committing the prerequisite.

- [ ] Obtain independent per-task and whole-change review. Verify the source diff excludes unrelated iCloud duplicates; push a draft PR against current `main`, wait for required checks, then merge under the owner's existing authorization.
- [ ] Prepare and independently review the exact bounded post-stop validator command/script using existing interfaces; this task adds no runtime helper. It must verify exact ConfigMap contents, a matching complete later collector JSONL frame, fresh two-node metrics, unchanged clean live client identities and adequate provider freshness before the exact finish in the same Workbench call. Pass the validated contract's original roster directly to `finish(..., expected_roster=original_roster)`, or its private mode-0600 file through `--expected-roster-file`; an unbound call or a roster inferred anew from live state is prohibited.
- [ ] Demonstrate that reviewed command in a fresh replacement-disabled no-scale long feed/collector check using the local status file: final reconciliation, later receipt, intentional feed stop with verified exit, the single post-stop validation/finish call, both finals and settling under the remaining 30-second provider budget, independent archive assessment, restoration and cleanup. No overlapping Workbench status calls while the feed is active, and no cloud scale action in this check. Failure or uncertain timing blocks paid rehearsal.
- [ ] Only if the long check passes and current one-operator/cost/time gates allow, perform one new disabled 2→3→2 rehearsal with no in-window restart when exact zero conditions hold; assess/retain and restore exact two. Only a new rehearsal PASS unlocks a new full request.
