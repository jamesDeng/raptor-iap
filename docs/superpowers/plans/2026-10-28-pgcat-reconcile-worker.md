# PgCat Reconciliation Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Produce one exact source-bound monitoring-target patch component for the reviewed paid close sequence.

**Architecture:** The existing source-pinned phase worker invokes a fixed contract function. Sealed baseline/Pending data supplies addresses; current authenticated provider evidence supplies exact two-node state; one resourceVersion/UID-bound patch and readback produce a private seal and address-free result. Paid runtime stays closed until later journal/controller integration and the complete closure graph.

**Tech Stack:** Python3.6-compatible worker code and fake unittest Kubernetes/provider fixtures.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-reconcile-worker-design.md`

## Global Constraints

- Existing checkout/branch, no clone/worktree; preserve unrelated iCloud/untracked files.
- No real cloud/network/credentials/billing/Raptor/deployment/staging in this task; tests use synthetic private fixtures only.
- Keep public paid release guards, source maps, catalog/controller/protocol/provider code unchanged.
- Fixed namespace `raptor-test`, ConfigMap `pgcat-provider-inventory`, key `targets.json`, port9930, exact scope labels from spec.
- One private intent before one patch; no retry/reverse or false cancellation claim. Preserve private evidence on failure.
- No whole-window capture scan inside the five-second action; component result is not traffic PASS.

## Review Focus

- Sidecar publication ahead of acknowledged local receipt must refuse before mutation (Task1).
- A removed node's address must come from sealed Pending, never current two-member map or caller (Task1).
- An old immutable Pending proof must remain usable as provenance without artificial freshness (Task1).
- Unexpected existing two-target ConfigMap must refuse rather than hide a previous uncertain patch (Task1).
- Patch acknowledged but failed readback remains consumed and cannot retry under a new invocation (Task1).

### Task 1: Fixed reconciliation producer and phase entry

**Files:** Modify `test-assets/traffic/pgcat_rehearsal_contract.py`, `pgcat_rehearsal_worker.py`; test `test_pgcat_rehearsal_contract.py`, `test_pgcat_rehearsal_worker.py`. No other source changes without reporting a concrete dependency first.

**Interfaces:** Implement exactly `reconcile_close(contract_dir, run_id, target, pending_binding, close_context, *, kubectl=None, clock=None)`. Context exact keys and canonical hash formula are in the spec. Read sealed Pending summary/input hashes and current sidecar; derive all target rows internally. `kubectl` defaults to current worker helper (namespace fixed) and supports GET/one merge PATCH/GET with normal bounded subprocess behavior; fake injection is tests only. Return exact `{feedSession,sequence,resourceVersion}`; retain full private intent/sealed result. Add fixed phase CLI verb with exactly eight args: verb, contract, run_id, kubeconfig, hashes, target JSON, Pending summary JSON, close context JSON. Execute opened verified imports, recheck source hashes after function, emit exactly the two legacy result rows on success; safe existing refusal output on failure.

- [ ] Add RED tests for happy one-patch exact argv/data/readback; summary/input/source hash mismatch; stale/ahead sidecar and ACK ordering; wrong current fleet/protection/backend; removed address mismatch; changed ConfigMap UID/RV/keys/targets/labels/port; already-two targets; duplicate intent; patch/readback failure; fixed private CLI output and unknown args/source refusal. Include Pending age well over30seconds and unchanged ConfigMap RV equal to the Pending record accepted when current source is fresh.
- [ ] Run both focused fixture modules, record expected RED failures before production changes.
- [ ] Implement the fixed read/validate/intent/patch/readback/seal sequence with existing private directory/sealed-file helpers. Check baseline bound kubeconfig equals the phase argument before reads; no caller target address input or generic mutation executor.
- [ ] Run focused tests normal and optimized, then full normal and optimized sequentially. Check changed production Python3.6 grammar and `git diff --check`. Diagnose and record existing short-deadline fixture transients; no unbounded retries or widening.
- [ ] Self-review, commit only four owned files, report RED/GREEN and limitations, obtain independent scoped spec/quality review. No runtime or full-window clearance follows.

## Downstream

The next unit must bind actual local scale-in ACK time, later exact-two receipt and one semantic reconciliation admission in the journal/supervisor/controller, validate the result, then require a later successful same-session publication before graceful paid feed stop. The worker context alone is not ACK proof. Remaining stop/finalization/verdict/abort/cutoff/full-graph review gates stay closed.
