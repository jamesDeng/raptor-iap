# PgCat Post-stop Finish Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax for tracking.

**Goal:** Validate fixed final provider/collector evidence and obtain one exact two-client durable finish boundary.

**Architecture:** A new fixed source-verified worker verb consumes asserted stop context, sealed original inputs and actual bounded collector witnesses. Existing helper reads/execs share one private shrinking deadline. This is a worker component; local journal/supervisor admission is the next unit and public paid release stays closed.

**Tech Stack:** Python stdlib, private JSON seals and Python3.6-compatible worker sources with fake Kubernetes/process/clock fixtures.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-post-stop-worker-design.md`

## Global Constraints

- Existing linked checkout/branch only; no cloud/network/credentials/billing/staging/Raptor/deployment/push in implementation.
- Preserve existing nine-key worker SOURCES constant, old entries, no-scale defaults/argv/artifacts, release guards, request keys and both NO_SCALE_ACTIONS.
- New fixed verb alone verifies the existing11-key collector union; no new source-map key or generic caller capability.
- One worker-local4s deadline at actual post_stop_finish function entry; unchanged collector original+5s; no per-step reset or whole-window scan.
- Later Mac admission requires original5s and final observedAt<30 on returned exact boundary. Timeout/late response is UNKNOWN, never cancellation/replay/PASS.
- No fleet/ConfigMap/provider/client restart mutation. Only one exact original-client private finish with exclusive durable intent and truthful uncertain outcomes.
- Derive fixed original roster/paths/provider/source/CM evidence from private sealed baseline; no caller path/command/roster/duration/PID proof.
- Ordinary whole archive remains sole traffic PASS; worker PREPARED means boundary completion only.

## Review Focus

- A caller's plausible stop context cannot substitute mismatched actual provider/reconciliation/source/tail evidence (Task1).
- Existing live reads must not reset deadlines or let two sequential3s operations become qualified success (Task1).
- One unknown parallel finish result consumes the intent and cannot be replayed (Task1).
- A recent progress boolean cannot hide partial/tampered bytes, adverse original SQL or missing metrics (Task1).
- Timely helper ACK without exact durable original final markers/start/receipt hashes cannot complete (Task1).

### Task1: Bounded exact worker finish

**Files:** Modify `test-assets/traffic/pgcat_rehearsal_contract.py`, `pgcat_rehearsal_worker.py`, `finish_clients.py`, `worker_capture.py`; matching four fixture modules. No collector-control/segmented implementation/source-map/release/local-lane changes without concrete dependency report. Execute only after previous witness source review.

**Interfaces:** `post_stop_finish(contract_dir,run_id,target,stop_context,*,source_hashes,kubectl=None,clock=None,monotonic=None)` uses spec's exact six-key stop context, eleven-key verified source map and six-key result. `post-stop-finish contract runId kubeconfig hashes target stop_context` fixed seven-value argv, source-verified before/after, exact baseline kubeconfig, fixed two-line worker output as spec. Existing nine-key calls stay unchanged. Build exact expected progress context from original sealed baseline/provider (read full bundleSha256 from fixed provider-source/run-context.json; fresh_provider_source validates it but does not return this field) and passed hashes/actual collector; match actual proc tuple to retained child without PID adoption. Reuse bounded read_progress and existing exact_clients/complete_metrics/fixed-original inventory checks; no whole-capture reader.

**Deadline helper interfaces:** Add optional private keyword `deadline=None` to worker_capture.get/kubectl/read_pod and finish_clients.live_roster/wait_ack/exec_finish/finish, keeping all old positional arguments/default behavior. For kubectl and finish/live_roster/exec_finish also use optional private kubeconfig=None from verified baseline and explicit immutable per-subprocess env; never temporarily mutate os.environ across parallel/timeout work. Use current worker time.monotonic for remaining budgets, check before/after operations, min(existing timeout,remaining), bounded sleeps and parallel exec. Tests may patch clocks/readers. post_stop_finish computes its own4s deadline; no CLI/context deadline. Do not block executor shutdown beyond remaining time, and do not claim remote cancellation.

- [ ] Write failing real-file/fake-I/O tests for successful exact original final boundary, strict context/argv/eleven-key source guards and unchanged old nine-key/no-scale calls; stale/wrong source/child/reconcile/CM/receipt; sticky gap/error/identity/skipped/SQL or tampered/incomplete tail; nonadvancing SQL and raw-metrics gaps; wrong original roster; exhausted budget before/after each operation, partial parallel ack, delayed thread after caller environment restoration using only bound credential env, duplicate intent, missing/late/mismatched boundary/start/request/receipt/finals, and legacy helper defaults.
- [ ] Run focused RED and retain feature-related failure evidence; no live resource operations.
- [ ] Implement fixed private validation/intent/one finish/result seals, exact bounded readback and optional deadline propagation. Errors sanitized; preserve consumed intent and partial evidence. All six result fields and both fixed JSON lines match spec. Local journal proof remains explicitly a downstream dependency.
- [ ] Run focused normal/O then full traffic normal/O sequentially on final source; Python3.6 grammar, source/guard invariance and diff hygiene. Diagnose short-deadline fixture variability; no widening/skip/retry loop.
- [ ] Self-review, commit only owned eight files, report RED/GREEN/schema/interfaces/limits and obtain independent scoped review. No live paid release or final PASS claim.

## Downstream

Fixed local post-stop role/admission must independently derive this stop context from Journal.rehearsal_stop(), reserve once before dispatch, enforce frozen receipt identity/freshness and five-second result deadline, and seal worker result. Abort/ordinary verdict/restoration/exact credential cleanup/independent finish, closure cutoff headroom, native observer and session handoff remain explicit later gates.
