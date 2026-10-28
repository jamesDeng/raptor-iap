# PgCat Target Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve existing provider target and ConfigMap readback refusals under optimized Python.

**Architecture:** Replace worker `_TARGET_SCRIPT` `assert` expressions with explicit `ValueError` guards. Fix the synthetic test boundary to raise explicitly, then test the actual embedded script with `python3 -O` before any live proof.

**Tech Stack:** Python 3.6-compatible embedded script, Python unittest, existing provider feed and rehearsal fixtures.

**Spec:** `docs/superpowers/specs/2026-10-26-pgcat-target-validation-design.md`

## Global Constraints

- No cloud/Workbench/ACK/ESS/NLB/Gateway/Raptor calls or credentials in this task.
- Keep exact `raptor-test/pgcat-provider-inventory`, resourceVersion patch and readback, 15-second absolute deadline and no write retry.
- Do not edit iCloud `* 2` duplicates or private scratch controller files.
- The existing failed rehearsal and full replacement remain INCONCLUSIVE/NOT PASS.

## Review Focus

- An optimized interpreter must reject mismatched final targets before publication: Task 1 regression.
- A wrong ConfigMap name/namespace or missing target key must refuse before patch: Task 1 regression.
- A wrong kubeconfig mode must refuse before any kubectl call: Task 1 regression.
- The fake boundary must fail under `-O` for an actual mismatch rather than lose its `assert`: Task 1 test.
- Error handling must not leak raw kubectl/provider output: Task 1 diff review.

---

### Task 1: Unconditional worker target guards

**Files:** Modify `test-assets/traffic/provider_feed.py` and `test-assets/traffic/test_rehearsal_mode.py`; add focused cases in `test-assets/traffic/test_provider_feed.py` or `test_rehearsal_mode.py` where the real embedded script boundary is already exercised.

**Interfaces:** `_TARGET_SCRIPT` retains the same arguments and stdout on success. Each prior `assert` becomes an explicit `if not CONDITION: raise ValueError(FIXED_CATEGORY)` with no raw payload in the category. The synthetic `configmap_remote` boundary raises explicitly on resourceVersion mismatch, even under `-O`.

- [ ] Write a failing optimized regression for incomplete/mismatched final readback and the other guard cases; run the focused test with `python3 -O` to observe RED.
- [ ] Replace only the five embedded guard sites and the test boundary assertion with explicit checks. Preserve exact success behavior and deadline plumbing.
- [ ] Run focused provider/rehearsal tests in normal and `-O`; run the full traffic suite once in each mode. Verify `git diff --check`, Python 3.6 grammar of worker source and no credential-bearing diff.
- [ ] Commit only Task 1 files, request independent code review and integrate via a source-only PR under the user's standing merge authorization. This does not start a live rehearsal.
