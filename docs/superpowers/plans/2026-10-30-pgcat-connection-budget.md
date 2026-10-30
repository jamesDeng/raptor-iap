# PgCat Connection Budget Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development; one source implementer at a time, independent task review.

**Goal:** Enforce owner-approved combined total connection budget1 consistently without expanding architecture.
**Architecture:** Reuse existing scoped Infra API/Prometheus reads, approval claims and direct-v2 paths; preserve legacy strict-zero semantics.
**Tech Stack:** Go, Node, Python3.6-compatible existing code/tests.
**Spec:** ../specs/2026-10-30-pgcat-connection-budget-design.md

## Global Constraints
Existing project/branch only. No new resources, endpoint, controller, credential or runtime admission bypass. Terraform minimum2 unchanged. Desired>=minimum; reductions exact approved once. Combined retiring active+idle+waiting<=1 across all pools/users, never per-node allowance. All existing freshness/completeness/replay protections retained. Existing native release remains closed; no cloud operations. No subagents spawned by workers. Parent owns docs/unrelated iCloud artifacts.

## Review Focus
- Two retiring nodes with one client EACH refuse (combined2).
- One waiting or idle client counts toward budget; protected survivors do not.
- Missing/stale/fractional/NaN data refuses even below threshold.
- Healthy serving remainder below live minimum refuses before withdrawal.
- Historical strict-zero records remain strict; direct-v2 policy changes only.

### Task 1: Infra API policy correction
Files: components/infra-api/internal/policy/proxy.go, proxy_test.go; matching internal/commands/direct_replacement_test.go and runtime_test.go fixtures if necessary. Existing metrics provider keeps complete all-pool total.
- [ ] RED tests for combined0/1 accepted,2 rejected across eligible nodes; idle/waiting total source retained, protected survivor clients excluded; preserve malformed/stale/duplicate/noeligible rejection.
- [ ] Update ScaleInClientsAllowed without signature change; sum eligible Clients and reject above1. Add explicit healthy registered serving remainder>=snapshot.Min in DeregistrationAllowed before existing majority/direct exception; malformed capacity refuses.
- [ ] Tests desired==minimum accepted/belowminimum refused; reduction with budget1 still requires approval and one-use claim, runtime SDK request still leaves MinSize/MaxSize nil.
- [ ] Run focused RED/GREEN then frozen full Infra Go tests/vet with writable /private/tmp cache; self-review/commit owned source/tests and report. Independent parent task review.

### Task 2: Direct harness policy correction
Modify replacement-sequence.mjs and direct tests/pinned current skill wording only. Aggregate eligible old-node all-pool connectedClients across both originals<=1 in direct-v2 drained gate; all triplets remain validated and freshness/after-restart requirements retained. Legacy staged paths unchanged. RED combined1 success/1+1 refusal and malformed/triplet negatives; full harness suite once frozen, review.

### Task 3: Direct assessment policy correction
Inspect capture_window.py direct transition branch and direct fixtures; adopt explicit direct-v2 combined retirement budget1 consistently, preserving historical zero behavior, full coverage and FAIL precedence. No new assessment framework. RED a single residual across two old nodes accepted while2refuses; preserve missing-series/stale/unknown/gaps refusal. If existing retirement proof cannot express owner budget without major redesign, report concrete conflict before editing. Run focused direct/legacy tests and full affected suite once frozen, review. Update current docs and test-only verification claim; do not enable or claim live PASS.

## Completion — source only
All three tasks and final bounded integration review completed at1c71b27. FullGo9packages+vet, Node292, Python114normal/114optimized/full980 passed. All8remoteCIworkflows passed for1c71b27. See planworkspace reports for exact commands/expected timing-fixture diagnostics. No deployment, native unlock, livePASS or claim of completedAPIreadrouting. LargerPR304 remainsdraft/unmerged.
