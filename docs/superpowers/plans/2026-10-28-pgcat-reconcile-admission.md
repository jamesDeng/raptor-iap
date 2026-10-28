# PgCat Reconciliation Admission Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Authorize one reconciliation only from an actual scale-in ACK and newer exact-two publication.

**Architecture:** Journal terminal/admission records supply immutable context to a fixed source-pinned worker command. Controller consumes a sealed result and later publication; provider retained history remains intact. Public paid runtime stays structurally disabled.

**Tech Stack:** Local Python3.9, Python3.6-compatible traffic syntax, SQLite transactions and fake subprocess/provider tests.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-reconcile-admission-design.md`

## Global Constraints

- Existing checkout/branch only; preserve unrelated files and other plan workspaces.
- No real cloud/network/credentials/billing/Raptor/deployment/source staging in implementation.
- Keep production `_release_admission`, NO_SCALE_ACTIONS, request keys and source maps unchanged; no enable flag.
- Fixed `reconcile-close` action, existing worker eight-argument contract and strict two-row result; five-second absolute owned worker budget.
- Current receipt age [0,3.5) seconds, desired two, exact originals, sequence/time after actual ACK. Old ACK/Pending provenance is not artificially refreshed.
- Admission/semantic claim before external dispatch; uncertain or duplicate outcomes never retry/reverse.
- No feed stop or whole-window PASS in this unit. Whole-graph full validation remains downstream.

## Review Focus

- A newer receipt during response sealing must not replace the immutable admitted sequence (Task1).
- A controller state or caller ACK assertion cannot authorize a patch without sealed journal ACK (Task1).
- New UUID replay is refused transactionally before spawning (Task1).
- Provider's next unchanged tick must not restore the removed target (Task1).
- Default no-scale verdict/admission/release behavior remains unchanged (Task1).

### Task 1: Actual ACK-bound reconciliation admission and successor

**Files:** Modify `test-assets/traffic/pgcat_lane_protocol.py`, `pgcat_lane_actions.py`, `pgcat_lane_supervisor.py`, `pgcat_rehearsal_controller.py`; their matching `test_pgcat_lane_protocol.py`, `test_pgcat_lane_supervisor.py`, `test_pgcat_rehearsal_controller.py`; add one fake regression in `test_provider_feed.py`. Do not modify reviewed worker/contract, provider production, maps or release guard without reporting a concrete dependency.

**Interfaces:** `Journal.scalein_ack()` returns its immutable actual terminal proof. `Journal.reconcile_context(receipt)` derives exactly the six existing worker fields, and `Journal.admit` stores `reconcile_admission` plus semantic claim in the same transaction before dispatch. `Journal.seal` gains optional `reconcile_source_sha=None`, valid only for this action and matching run's rehearsalContract digest; stores `reconcile_binding()` from admission/response/source. Use these three private tables/proofs or equivalent focused schema preserving existing journal compatibility. Supervisor supplies context/Pending from actual journal and passes actual receipt to admit; catalog selects only fixed private worker args and keeps strict legacy result shape. Controller adds `reconcile_rehearsal()`, mode/state/freshness checks, journal comparison, later desired-two successor and `reconciled-two` state. Existing `_worker_call` can own the live-feed action; its post-stop use remains forbidden.

- [ ] Write RED tests for missing/wrong ACK or run/session; old receipt/time/roster; invalid source/result; duplicate UUID semantic replay before spawn; response seal using immutable admitted rather than newer receipt; exact command/two-row parser; fake ACK→later2→worker→later2→reconciled-two flow; no-scale refusals; retained-three provider cache after explicit two-target reconciliation.
- [ ] Run focused tests for owned fixture modules and record feature RED failures, correcting only demonstrable fixture setup mistakes before evaluating implementation.
- [ ] Implement actual ACK sealing, context derivation/admission, fixed command/result seal, controller transition and retained-cache test. No direct operator CLI while feed runs, no caller context in request, no production guard changes.
- [ ] Run focused normal/optimized, full normal then optimized sequentially, changed-source Python3.6 grammar and `git diff --check`. Diagnose/document bounded short-deadline transients; no unbounded retries/widening. A prior worker unit's normal full-suite fixture error remains a release-validation limitation until a later complete source run is clean.
- [ ] Self-review, commit only owned files and report RED/GREEN, exact counts, scope and remaining guards. Obtain independent scoped review before next paid closure unit. No live clearance.

## Downstream

Next: barrier-bound paid feed start/stop and actual exit; distinct post-stop exact-original client validation/finish; whole-window ordinary assessment, cleanup/abort and cutoff ownership; final broad source review/CI/staging and a fresh same-source no-scale proof. Do not remove public paid guards until all are complete and reviewed.
