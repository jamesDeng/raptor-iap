# PgCat Rehearsal Continuity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make one new disabled 2→3→2 rehearsal capable of retaining strict, continuous evidence without a disruptive client restart or Workbench status polling.

**Architecture:** Keep the existing provider and collector. Retire metrics for a formerly registered node only after fresh complete zero and a later provider observation of NLB absence; keep every current backend mandatory. Add an optional private local publication receipt to the Mac-side provider feed so final reconciliation can be observed without another Workbench call. The runbook skips client restart only when exact zero/identity/traffic gates pass and stops the feed just before final finish.

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
- Reconciliation and finalization must not rely on a local receipt alone: Task 3 runbook check against the live spec.

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

**Interfaces:** No runtime code interface. The procedure requires the two fresh zero samples and unchanged client processes, one exact 3→2 action, final ConfigMap readback, later receipt plus collector frame, intentional feed stop, then exact finish within 30 seconds of the last provider observation.

- [ ] Replace the unconditional in-window client restart with the zero-confirmed no-restart path and a fail-closed outside-window recovery branch for nonzero/changed clients.
- [ ] Document the local receipt as liveness only, with no Workbench status polling after final reconciliation; require a worker frame and ConfigMap readback before feed stop.
- [ ] Run `PYTHONPATH=test-assets/traffic python3 -m unittest discover -s test-assets/traffic -p 'test_*.py' -v` and `git diff --check`; confirm no failures, then commit.

### Task 4: Review, merge and proof gates

**Files:** No additional source edits unless review finds a concrete issue.

- [ ] Obtain independent per-task and whole-change review. Verify the source diff excludes unrelated iCloud duplicates; push a draft PR against current `main`, wait for required checks, then merge under the owner's existing authorization.
- [ ] Run a fresh replacement-disabled no-scale long feed/collector check using the optional local status file, exact client finish, independent archive assessment and cleanup. No cloud scale action in this check.
- [ ] Only if the long check passes and current one-operator/cost/time gates allow, perform one new disabled 2→3→2 rehearsal with no in-window restart when exact zero conditions hold; assess/retain and restore exact two. Only a new rehearsal PASS unlocks a new full request.
