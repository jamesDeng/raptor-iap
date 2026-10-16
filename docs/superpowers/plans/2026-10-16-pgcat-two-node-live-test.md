# Controlled PgCat two-node live test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prepare a guarded, observable full 2→4→3→2 PgCat replacement test without enabling replacement or moving credentials during implementation.

**Architecture:** Keep the existing replacement controller and immutable skill; add an operator-side provider feed, one continuous worker traffic capture, and a narrow live Secret gate. Verify them with fixtures and a disabled-mode capture, then use separate action-time authorization for the credential transfer and live run.

**Tech Stack:** Python 3 standard library and `unittest` for the collector/feed; Go standard library and existing Gateway PostgreSQL client for the gate/idle check; existing `kubectl`, Alibaba CLI, Workbench CLI, Helm, GitOps, and OSS evidence path.

**Spec:** `docs/superpowers/specs/2026-10-16-pgcat-two-node-live-test-design.md`

## Global Constraints

- Exact scope: `rdev.ali`, `DP00001`, `D00002`, `A00003`, ESS `asg-t4necb4dfmu6o3z2rn0g`, NLB group `sgp-xs5s42tihtl6ja2a67`, ACK worker `i-t4nj1bitmcz7cuoch82h`. Rediscover original IDs before execution.
- `replacementEnabled=false` through implementation and disabled-mode capture. The earlier native qualification PASS is separate from replacement and traffic acceptance.
- Full run uses a **new** immutable replacement request, `gpt-5.6-luna`, pinned `skills-v0.4.0`, two distinct request-bound human approvals, and one continuous traffic window.
- Poll client and metrics evidence every second, provider inventory at least every 15 seconds; provider age >30 seconds, metrics source age >45 seconds, or a poll gap >3 seconds prevents PASS.
- Rotate segments at ≤30 minutes, bound the complete capture to ≤90 minutes, and require a completion manifest with hashes and an explicit final marker.
- No secret payload, DSN, SQL payload, token, or kubeconfig in Git, logs, shell arguments, receipts, retained evidence, or Terraform state. Do not read the older private Secret snapshots as update input.
- No new cloud resources or IAM through CLI/console. Required provisioning uses Terraform PR plan/comment and reviewed apply; application release uses existing image-pin/Argo GitOps flow.
- Do not transfer a kubeconfig or enable replacement without separate, specific action-time authorization. Follow `working/skills/ack-workbench-cli/SKILL.md` for ACK worker access; if that transfer is rejected, stop.
- Live run begins by 21:30 Asia/Shanghai, stops new mutations by 22:45, precedes the 23:59 owner stop, and stays within the RMB 1,000 ceiling.

## Review Focus

1. A Prometheus response contains the right IDs alongside a foreign or duplicate series: reject the mixed response, rather than select convenient rows (Task 1 tests).
2. Provider pages or NLB cursors change during a read: fail closed and leave the last published inventory intact but visibly stale (Task 2 tests).
3. A segment boundary coincides with a Pod restart or approval wait: preserve process/event identity and require unbroken scheduling coverage (Task 3 tests).
4. The live Secret changes between read and replace, or its field schema differs: reject without writing or disclosing any Secret bytes (Task 4 tests).
5. A paused/queued Gateway attempt exists when a flag rollout is requested: block the update and rollout until an authoritative idle read succeeds (Task 4 tests).

---

### Task 1: Exact retained metrics and client evidence

**Files:** Modify `test-assets/traffic/worker_capture.py`, `test-assets/traffic/test_worker_capture.py`; update `test-assets/traffic/README.md` with the exact labels.

**Interfaces:** Preserve `query_metrics(base: str, stamp: str) -> dict` and `capture_frame(inventory_path: str, final_snapshots: dict | None) -> dict` for Task 3. Require `env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"` on `up` and pool queries. Keep raw `sourceAt` timestamps and return only `node,pool,user,kind,value,sourceAt` rows; `capture_window.clean` remains the allowlist.

- [ ] **Step 1: Add failing fixtures** in `test_worker_capture.py`: exact scoped Prometheus queries; reject foreign/duplicate/missing `up` nodes, duplicate `(node,pool,user,kind)` series, missing timestamp pairing, nonfinite/nonintegral values, and absent `test/poc_app` triplets; preserve final snapshots only for matching Pod UID.
- [ ] **Step 2: Run RED:** `python3 -m unittest discover -s test-assets/traffic -p 'test_worker_capture.py' -v`; the new tests must fail for the intended missing validation.
- [ ] **Step 3: Implement minimal validation** in `query_metrics` and keep metric errors as unavailable evidence in `capture_frame`; update README label contract.
- [ ] **Step 4: Run GREEN:** the Task 1 command plus `python3 -m unittest discover -s test-assets/traffic -p 'test_capture_window.py' -v`; all pass.
- [ ] **Step 5: Commit** only Task 1 files with `fix: scope retained PgCat metric evidence`.

### Task 2: Bounded authoritative provider feed

**Files:** Create `test-assets/traffic/provider_feed.py`, `test-assets/traffic/test_provider_feed.py`; modify `test-assets/metrics/provider_inventory.py` and its tests only if a reusable pagination validator is needed; update `test-assets/traffic/README.md`.

**Interfaces:** `snapshot(fetch_ess, fetch_group, fetch_ecs, fetch_nlb, *, group_id: str, server_group_id: str, now: datetime) -> dict` returns exactly `observedAt`, integer `desired`, sorted complete `members`, sorted `healthyRegistered`, and a private-address map used only for metrics target reconciliation. `publish(snapshot: dict, previous_targets: tuple, atomic_worker_write: Callable, update_targets: Callable) -> tuple` sends allowlisted inventory through `atomic_worker_write` to a nonsecret 0600 worker file and calls `update_targets` only when verified membership changes. The production adapters call the existing Alibaba CLI profile on the Mac and documented Workbench CLI for the exact worker; never interpolate response text into shell code. `capture_frame` consumes the worker inventory file from Task 1.

- [ ] **Step 1: Add failing fixtures** for complete 2/3/4-member ESS pagination, changing count/page/cursor, foreign group/server group, duplicate member/backend, transitional/unhealthy nodes, wrong VPC or missing private ECS address, and NLB health mismatch. Test that a failed refresh does not publish a guessed roster; only changed verified membership updates the existing `pgcat-provider-inventory` ConfigMap.
- [ ] **Step 2: Run RED:** `python3 -m unittest discover -s test-assets/traffic -p 'test_provider_feed.py' -v`.
- [ ] **Step 3: Implement `snapshot`/`publish` and the bounded `--interval 15 --max-seconds 5400` operator loop** with injected read/publish adapters, explicit `ap-southeast-1` and IDs, atomic same-directory replacement, and sanitized failures. Stop on provider/Workbench failure rather than silently extend freshness. Do not create or modify a cloud resource.
- [ ] **Step 4: Run GREEN:** the Task 2 command plus `python3 -m unittest discover -s test-assets/metrics -p 'test_provider_inventory.py' -v`; all pass.
- [ ] **Step 5: Commit** only Task 2 files with `feat: publish bounded PgCat provider evidence`.

### Task 3: One continuous segmented assessment

**Files:** Create `test-assets/traffic/segmented_capture.py`, `test-assets/traffic/test_segmented_capture.py`; modify `test-assets/traffic/worker_capture.py`, `test-assets/traffic/README.md`.

**Interfaces:** `SegmentedCapture(directory: Path, max_segment_seconds: int = 1800, max_total_seconds: int = 5400)` owns **one** `CaptureWindow` sequence state across rotations; `append(frame: dict) -> None`, `finish(*, completed: bool, cancelled: bool, errors: list) -> dict` write 0600 JSONL segments and a 0600 completion manifest with ordered path/basename, SHA-256, first/last timestamps, missing-poll count, and explicit client final markers. `assess_manifest(directory: Path) -> dict` verifies hashes/order/bounds/final markers and feeds all frames, once, into `capture_window.assess`. The worker CLI selects a fresh private directory and a 5–5400 second total duration; it performs one settling phase of ≤5 seconds at the end.

- [ ] **Step 1: Add failing fixtures**: two segments across a 4→3 approval wait and a 3→2 approval wait; rotation exactly at 1800 seconds; Pod restart on a boundary; missing/overlapping/reordered/tampered segment; >3 second gap; lost final marker; cancellation; a failed SQL operation; a clean second segment following a bad first segment. Only a complete unbroken fixture may PASS.
- [ ] **Step 2: Run RED:** `python3 -m unittest discover -s test-assets/traffic -p 'test_segmented_capture.py' -v`.
- [ ] **Step 3: Implement the segment writer/reader and worker CLI integration** without restarting event sequence state or a one-second poll clock. Keep unavailable polls in the manifest and make them INCONCLUSIVE unless an independently observed failure already yields FAIL. Never rewrite a completed segment.
- [ ] **Step 4: Run GREEN:** `python3 -m unittest discover -s test-assets/traffic -p 'test_*.py' -v`; all pass. A local synthetic 31-minute fixture must produce two ordered segment hashes and one assessment.
- [ ] **Step 5: Commit** only Task 3 files with `feat: assess continuous segmented traffic window`.

### Task 4: Guarded live Gateway flag and idle proof

**Files:** Create `components/agent-gateway/internal/execution/idle.go`, `components/agent-gateway/internal/execution/idle_test.go`; modify `components/agent-gateway/cmd/gateway/main.go`; create `tools/gateway-live-gate/go.mod`, `tools/gateway-live-gate/main.go`, `tools/gateway-live-gate/main_test.go`.

**Interfaces:** A Gateway `--idle-check` mode reads only `gateway.attempts` with `ended_at IS NULL`, queued/active `gateway.executions`, and the held `gateway.runtime_slot`; it exits nonzero on any row or query failure and prints only `idle=true` when all are empty. The worker-side `gateway-live-gate --namespace raptor-system --secret gateway-live-config --expected false --set true` (or expected true/set false) runs idle check via `kubectl exec`, reads the **live** Secret JSON into memory through `kubectl get`, validates exact target, key, live config scope and boolean, and submits one `kubectl replace -f -` using the observed `resourceVersion`. It never prints child output or Secret bytes. A separate `verify` command reports only boolean flag, Deployment generation/readiness, and a new ready Pod UID started after the Secret change; it never reveals config content. No old JSON snapshot path is accepted.

- [ ] **Step 1: Add failing Go tests** for idle, queued, running and paused attempts, unavailable DB; exact Secret patch changes only decoded `replacementEnabled`; wrong namespace/name/key/scope, missing boolean, unexpected config fields, stale resourceVersion/conflict, malformed JSON, sanitized stdout/stderr, and `true→false` restoration. Fake command runners must record that `kubectl replace` is never called on a rejected case.
- [ ] **Step 2: Run RED:** `go test ./internal/execution ./cmd/gateway` from `components/agent-gateway`; `go test ./...` from `tools/gateway-live-gate`.
- [ ] **Step 3: Implement the Gateway idle query and flag CLI** using the existing DB connection pattern and Go standard library subprocess calls. Validate exact account/region/group/server group/application binding from the decoded live config; compare all other decoded fields before submission. Pass Secret JSON only through stdin. Require a fresh idle check immediately before replace and before any explicit rollout; stop on a conflict, never retry with a stale read.
- [ ] **Step 4: Run GREEN:** both Task 4 commands; build a Linux binary for the detected worker architecture, and inspect it for dependency on no private file. Do not upload or execute against the live cluster at this stage.
- [ ] **Step 5: Commit** only Task 4 files with `feat: guard live Gateway replacement flag`.

### Task 5: Contract and packaging regression

**Files:** Modify only existing focused tests if a gap is confirmed: `components/agent-harness/approval-pause.test.mjs`, `components/agent-harness/replacement-completion.test.mjs`, `components/infra-api/internal/approval/submission_test.go`, and related exact fixtures. Create `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md` for the operator sequence.

**Interfaces:** No change to the immutable `skills-v0.4.0` tag or existing replacement API. The runbook consumes Task 1–4 artifacts and emits a sanitized evidence receipt: request ID/definition SHA/skill SHA, action IDs and approval decisions, fleet before/after, collector manifest hashes, assessment status, cleanup checks. It does not include credentials.

- [ ] **Step 1: Add only missing regression fixtures** for distinct exact 4→3 and 3→2 approvals, pause/resume binding, unknown action acknowledgement with later reconciliation, and convergence result `acceptance: pending`. If existing tests already prove a case, cite their test name in the runbook rather than duplicating it.
- [ ] **Step 2: Run RED** for any new tests using `node --test approval-pause.test.mjs replacement-completion.test.mjs` from `components/agent-harness` and `go test ./internal/approval` from `components/infra-api`; verify failures are for the missing contract.
- [ ] **Step 3: Make the smallest contract correction if RED exposed one, then write the runbook:** exact preflight/cost/time gates; signed-in Raptor; worker runtime and profile check; disabled 60-second capture; source/binary/image/skill hashes; full-window start before flag change; guarded enable, rollout and new request; two separate approval pauses; authoritative convergence; full-window assessment; guarded disable only after settlement; encrypted OSS retention and local/worker kubeconfig deletion on all paths. Any newly required IAM returns to Terraform review.
- [ ] **Step 4: Run GREEN** for targeted tests, then the affected Go/Node/Python suites, `helm template` for `rdev.ali`, and relevant GitOps/image checks. Document commands and exact results in the runbook; do not claim PASS from an unrun command.
- [ ] **Step 5: Commit** Task 5 changes with `docs: gate two-node live test execution`.

### Task 6: Review, release, disabled-mode proof, and action gate

**Files:** No source changes by default; append a sanitized, dated verification receipt to the runbook. Any necessary release pin change follows the existing GitOps files and review.

**Interfaces:** Produces a reviewable ready-to-run bundle, **not** a replacement PASS. The live test has its own action-time go/no-go decision; no code task implies approval to transfer a kubeconfig, enable the flag, or create the request.

- [ ] **Step 1: Review the whole diff** against the spec, threat boundaries, credentials, resourceVersion, full-window lifecycle, and the five Review Focus cases; fix concrete findings and rerun affected tests.
- [ ] **Step 2: Use the existing PR/image pin/Argo flow** to deploy reviewed code, verify image digest and `skills-v0.4.0` SHA, and verify Gateway remains `replacementEnabled=false`. Terraform plan/comment and reviewed apply are required only if provisioning changed.
- [ ] **Step 3: Re-establish local Alibaba CLI/Workbench authentication as needed without disclosing credentials;** read worker architecture/Python/kubectl availability and ACK target. If Python, profile, private reachability, or authorized kubeconfig is unavailable, stop before capture. Any kubeconfig transfer needs its own specific authorization even for disabled-mode capture.
- [ ] **Step 4: After the transfer is authorized, perform a 60-second disabled-mode capture** with exact healthy baseline, 2/2 clients and 1/1 metrics, direct Pod/Prometheus reachability, fresh provider feed, explicit completion, and private retention. Check hashes and delete temporary kubeconfig files. Treat this as capture-path proof only.
- [ ] **Step 5: Present the exact live target, current cost/time, baseline, worker artifact and duration, flag update and cleanup commands, and evidence receipt for separate action-time authorization.** Start the full two-node run only after that authorization; otherwise stop with `replacementEnabled=false` and report the blocker.
