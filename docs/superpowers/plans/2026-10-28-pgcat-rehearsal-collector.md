# PgCat Rehearsal Collector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Supply a fixed boundary-driven long collector component while keeping paid runtime structurally disabled.

**Architecture:** Two immutable private collector profiles share the existing exact-child guardian and source-snapshot loader. No-scale retains its current behavior; rehearsal uses the existing baseline's capture/boundary paths. Paid catalog/controller integration is a later unit of Task6D, after this component is reviewed.

**Tech Stack:** Python3.6-compatible worker code, MacPython3.9, unittest.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-rehearsal-collector-design.md`

## Global Constraints

- Existing checkout/branch only; preserve unrelated iCloud files and4existing untracked design/plan documents.
- No cloud calls, credentials, provisioning, Raptor actions, source staging or live tests in this plan.
- Keep all existing structural paid admission refusals; never add an enable flag.
- No-scale65seconds/collector-capture/guardian80/status90 remain unchanged.
- Rehearsal5400seconds/rehearsal-capture/disabled-boundary, guardian5415/status5430; segment maximum1800seconds.
- Exact retained process identity/ALIVE observation/exit seal, immutable intent and verified opened sources remain mandatory.
- Component completion is not evidence PASS; later ordinary whole-window assessment remains required.

## Review Focus

- Same UUID/profile change cannot adopt an existing collector-job (Task1).
- Guardian sees child exit before first status: missing observedALIVE still refuses (Task1).
- Boundary/final marker missing at duration cap cannot produce successful capture completion (Task1).
- Long status during run must not reuse no-scale90-second timeout (Task1).
- Direct/transitive stale bytecode must remain ignored in both private profiles (Task1).

### Task 1: Fixed long collector lifecycle

**Files:** Modify `test-assets/traffic/pgcat_collector_control.py`; test `test-assets/traffic/test_pgcat_collector_control.py`. Touch another test only for a demonstrated unchanged-API fixture dependency; report it before editing. No controller/protocol/catalog/admission changes in this unit.

**Interfaces:** Preserve existing positional call compatibility for `launch`, `run_child`, `status`, `completion`, `capture_arguments` and `intent`; append a keyword-only `profile='no-scale'` where needed. Accept only `no-scale` and `disabled-rehearsal`. Private verbs map to these constants; `_rehearsal-run` and `_rehearsal-capture` propagate the sealed paid profile, never a caller duration. `launch`/status payloads stay unchanged. Paid intent adds exactly `profile:'disabled-rehearsal'`; no-scale intent remains exact existing schema. Wrong-profile reads refuse. Profiles share one fresh collector-job and duplicate launch refuses.

- [ ] Add failing tests `test_rehearsal_capture_argv_uses_bound_paths_and5400`, `test_no_scale_profile_remains65_without_boundary`, `test_rehearsal_intent_binds_profile_across_child_and_status`, `test_cross_profile_status_and_launch_refused`, `test_rehearsal_guardian_and_status_use_long_budgets`, `test_rehearsal_missing_final_or_expired_duration_is_not_completed`, and matching verified-source fixtures. Add `test_rehearsal_launch_refuses_missing_nonempty_or_nonprivate_boundary` asserting refusal before intent/spawn while no-scale stays unchanged. Use existing fake subprocess handles; no real cloud calls or5400-second waits.
- [ ] Run `python3 -m unittest discover -s test-assets/traffic -p 'test_pgcat_collector_control.py'` and record the relevant RED failures.
- [ ] Implement the fixed profile mapping and sealed propagation using the current opened-source entry, exact guardian identity and one-use directory. Validate the expected completion maxTotalSeconds/path/segment bound for each profile. Keep exact owned-child timeout reaping and unchanged no-scale behavior.
- [ ] Run focused tests normal and `python3 -O`; then `python3 -m unittest discover -s test-assets/traffic -p 'test_*.py'` and its optimized equivalent. Compile changed production source with Python3.6 grammar and run `git diff --check`.
- [ ] Self-review and commit only the scoped component/tests. Write report with RED/GREEN, counts, commit and remaining limitations. Obtain an independent scoped spec/quality review before any next unit. No live clearance or paid guard removal.

## Downstream, outside this bounded plan

Next units are fixed catalog/controller profile integration with day cutoff; exact ConfigMap reconciliation; barrier-bound paid feed stop/exit; distinct post-stop original-client validation/finish; authenticated whole-window verdict/abort/cleanup; complete path review and only then paid admission release. Each needs its own tested interface and review. Do not restart completed no-scale tasks or remove guards merely because this component passes.
