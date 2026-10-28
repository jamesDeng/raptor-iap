# PgCat Rehearsal Collector Lane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Bind the fixed rehearsal collector to the existing owned lane without enabling paid runtime.

**Architecture:** Two fixed paid action names dispatch the reviewed component profile through the same supervisor. Journal mode and exact collector identity bind results; the controller chooses actions by mode and waits for a later publication at the current desired count. Existing no-scale behavior is preserved.

**Tech Stack:** Python 3.6-compatible traffic modules, Mac Python 3.9 and unittest fixtures.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-rehearsal-collector-lane-design.md`

## Global Constraints

- Existing checkout and branch only; preserve unrelated iCloud/untracked files and parent-owned docs.
- Prerequisite: collector component scoped review cleared. This plan does not modify that component.
- Keep every production paid release guard structurally no-scale-only; no enable flag or bypass.
- Exact action names `rehearsal-collector-launch/status`; exact private verbs `rehearsal-launch/status`.
- Existing five-second absolute worker budget, source verification, one-use request, shared lock and post-call successor observation remain.
- No cloud/network, credentials, provisioning, Raptor actions, staging or live tests during implementation.
- Source-only/offline completion does not certify a paid rehearsal or replacement.

## Review Focus

- Patched release guards in offline tests must not disable independent journal mode/profile checks (Task 1).
- A paid status call at desired three must require a later desired-three publication and retain current receipt freshness (Task 1).
- Old no-scale collector records must never supply a paid exit or vice versa (Task 1).
- A new UUID cannot re-dispatch the paid collector launch (Task 1).
- Public no-scale API called on a paid controller must refuse before dispatch (Task 1).

### Task 1: Fixed paid collector lane actions

**Files:** Modify `test-assets/traffic/pgcat_lane_actions.py`, `pgcat_lane_protocol.py`, `pgcat_rehearsal_controller.py`; corresponding `test_pgcat_lane_supervisor.py` (catalog/parser fixtures), `test_pgcat_lane_protocol.py`, `test_pgcat_rehearsal_controller.py`. Supervisor source only if a concrete required mode-guard call site is identified; record the reason before editing. Do not modify collector component, provider feed, source maps or release guards.

**Interfaces:** Add the two exact action strings to ACTIONS, never NO_SCALE_ACTIONS. Catalog command uses unchanged fixed source-pinned wrapper and current bindings, selecting the exact private verb; parser payload schemas match the existing collector launch/status schemas. Journal admission enforces collector pair to run mode and one paid launch per run. `Journal.collector_exit(*, profile='no-scale')` and internal query validate profile and run mode and select the matching status action; default existing calls remain unchanged. Journal status sealing retains matching profile identity/ALIVE-before-EXITED. Controller `_baseline_collector` chooses mode-specific pair via `_worker_call`; `call_no_scale` explicitly rejects other modes. `_worker_call` waits for a strictly newer publication with `desired=receipt['desired']` and still checks observed time after call; the paid live-feed status path accepts only ALIVE. No post-stop finalization or paid public release in this task.

- [ ] Write failing tests for `test_rehearsal_collector_action_argv_and_parser`, `test_collector_pair_requires_matching_journal_mode`, `test_rehearsal_collector_launch_is_semantically_once`, `test_collector_exit_cannot_cross_profiles`, `test_paid_baseline_uses_long_profile_actions`, `test_paid_collector_status_waits_for_desired_three_successor`, and `test_public_no_scale_api_refuses_paid_controller`. Include malformed status, changed identity and missing ALIVE, plus an unchanged no-scale exit/verdict assertion.
- [ ] Run focused normal tests for the three fixture files; record relevant RED output.
- [ ] Implement only the fixed command/parser, mode-bound admission/identity query and controller orchestration. Use existing fake release patches only inside tests, never a runtime switch.
- [ ] Run focused normal and optimized tests. Run full traffic discovery normal then optimized sequentially; do not run timing-sensitive full suites concurrently. Check changed production Python 3.6 grammar and `git diff --check`. Record any existing transient fixture failures rather than hide them with unbounded retries.
- [ ] Self-review and commit only owned files. Report RED/GREEN, exact test evidence, source scope and remaining guards. Obtain independent scoped spec/quality review before the next closure unit. No live clearance follows.

## Remaining paid closure

Exact reconciliation, barrier-bound paid feed stop/exit, dedicated post-stop validation/finish, whole-window assessment/abort/cleanup and cutoff integration remain separate work. Only their complete reviewed graph can remove paid admission refusals, followed by exact source staging and a fresh no-scale proof before any paid rehearsal. Whole paid graph review is still required even after each unit passes.
