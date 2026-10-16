# PgCat Client Finalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close one retained PgCat traffic window with exact final markers from every observed test-client process, then restore two healthy clients.

**Architecture:** An in-Pod Unix socket accepts an exact Pod UID and process ID finish command; the client stops scheduling, drains work, finalizes, and serves evidence for two minutes before exit. A worker-side boundary protocol starts the existing five-second settling phase before two parallel exact-Pod commands, and the collector closes only after all observed processes have final markers. The existing two-replica GitOps Deployment restores clients.

**Tech Stack:** Go 1.27.1, Go standard library Unix sockets, Python 3 standard library and `unittest`, Helm, existing GitOps and immutable GHCR image flow.

**Spec:** `docs/superpowers/specs/2026-10-16-pgcat-client-finalization-design.md`

## Global Constraints

- Target only the `rdev.ali` traffic client for `A00003` / `DP00001` / `D00002`; keep `replacementEnabled=false` during disabled-mode verification.
- Keep the Deployment desired replica count at two, with Argo self-heal; add no TCP finish endpoint, cloud resource, IAM role, RBAC permission, or Secret.
- Match exact Pod UID and process ID before finish. One command per identity; reconcile an unknown acknowledgement from evidence instead of blindly repeating it.
- Stop new scheduling without cancelling already queued or active work; keep SQL deadlines finite and do not create new `Skipped` work through controlled finish.
- Retain one capture stream across rotations and approvals; settling is at most five seconds and the client final-evidence hold is two minutes.
- A missing/late final, identity change, poll gap, or uncertain lifecycle prevents PASS; an observed failed, timed-out, or ambiguous SQL operation yields FAIL.
- A live worker kubeconfig transfer, finish operation, Gateway flag change, and replacement each require specific action-time authorization. Do not perform these in offline implementation tasks.

## Review Focus

1. Same Pod UID with a restarted container: test that the collector invalidates the old cache on container ID change and exposes the new process.
2. Finish arriving during a queued operation: test that queue drain emits an operation, not `skipped`, before the final marker.
3. Lost `kubectl exec` response after the client accepted finish: test reconciliation by exact UID/process evidence without a second command.
4. One client finishes while the other remains active: test a settling frame with both identities and no false measurement-roster failure.
5. Pod leaves or hold expires before final evidence is captured: test INCONCLUSIVE and independent FAIL precedence when a SQL failure was already observed.

---

### Task 1: Private client finish control

**Files:**
- Create: `components/test-client/internal/control/control.go`, `components/test-client/internal/control/socket.go`, `components/test-client/internal/control/control_test.go`, `components/test-client/cmd/client/finish_test.go`
- Modify: `components/test-client/cmd/client/main.go`, `components/test-client/internal/traffic/runner_test.go`, `components/test-client/README.md`

**Interfaces:**
- `control.New(podUID, processID string) (*control.Controller, error)`; `Controller.Done() <-chan struct{}` closes once on a matching finish; `Controller.MarkFinal()` records completion.
- `control.Serve(ctx context.Context, socketPath string, controller *Controller) error` binds a mode-0600 Unix socket; `control.Request(ctx context.Context, socketPath string, podUID, processID string) (control.Status, error)` is the `/client finish` subcommand transport.
- `control.Status` has only `accepted`, `finishing`, or `final` and contains no credential or SQL data. Wrong identity returns an error without state change.
- Main's controlled tick source closes on `Controller.Done()` while the Runner's context stays active. After `Runner.Run`, main finalizes evidence, calls `MarkFinal`, serves HTTP for two minutes, and exits. SIGTERM remains the existing bounded cancellation path.

- [ ] **Step 1: Write RED tests** for wrong/empty UID or process ID, private socket mode, concurrent duplicate command, unknown response reconciliation status, and a queue containing a scheduled operation at finish. Assert no new `Skipped`, `Scheduled = Attempts`, `Connected = 0`, and final evidence remains readable while readiness is false and liveness is true.
- [ ] **Step 2: Run RED:** `cd components/test-client && go test ./internal/control ./internal/traffic ./cmd/client`. Expected: new control contract or assertions fail.
- [ ] **Step 3: Implement the exact interfaces** above using a short, bounded request/response protocol over the Unix socket. Wire the distroless `/client finish --expected-pod-uid UID --expected-process-id PID` subcommand before DSN loading. Keep the existing SIGTERM behavior and redact all runtime errors.
- [ ] **Step 4: Run GREEN and race checks:** `cd components/test-client && go test -race ./internal/control ./internal/traffic ./cmd/client && go vet ./...`. Expected: all pass. Then run `go test ./...`; report any environment-only integration skip separately.
- [ ] **Step 5: Commit** client control, tests, and README with `feat: add exact client finish control`.

### Task 2: Worker boundary and complete-process capture

**Files:**
- Create: `test-assets/traffic/finish_clients.py`, `test-assets/traffic/test_finish_clients.py`
- Modify: `test-assets/traffic/worker_capture.py`, `test-assets/traffic/test_worker_capture.py`, `test-assets/traffic/test_segmented_capture.py`, `test-assets/traffic/README.md`

**Interfaces:**
- `finish_clients.finish(boundary_dir: Path, expected_replicas: int = 2) -> dict` reads exact live Pod names/UIDs/container IDs/process IDs, writes one private boundary request, waits for collector acknowledgement, then dispatches two bounded `kubectl exec` calls concurrently. It returns a sanitized receipt keyed by UID/process; it never retries an uncertain call. CLI: `finish_clients.py --boundary-dir DIR --expected-replicas 2`.
- `worker_capture.run_capture(output: Path, inventory: Path, seconds: int, boundary_dir: Path | None = None) -> dict` is the testable extraction of the existing loop. With a boundary, it acknowledges a matching exact roster only after appending the first settling frame. It closes on all observed process finals or at five seconds; without a boundary, the existing timed capture behavior remains. CLI adds `--boundary-dir DIR` to existing flags.
- `read_pod(p: dict, final_snapshots: dict[tuple[str, str], dict]) -> dict` caches a final snapshot by `(pod UID, container ID)`, never by Pod UID alone; a new container in the same Pod is read as a new process. The manifest still binds `(pod UID, process ID)` finals.

- [ ] **Step 1: Write RED fixtures** for exact two-client roster, changed/missing Pod UID or process ID, container restart in the same Pod, boundary acknowledgement before commands, one final while the other is active, uncertain exec response with read-only reconciliation, both finals within five seconds, late/missing final, a new process after the boundary, and observed SQL failure plus missing final.
- [ ] **Step 2: Run RED:** `python3 -m unittest discover -s test-assets/traffic -p 'test_*.py'`. Expected: the new finish/boundary cases fail.
- [ ] **Step 3: Implement the worker interfaces** above. Use private atomic files in a mode-0700 boundary directory, an exact roster and one request ID, one-second polling, and the existing `SegmentedCapture` object throughout. Do not persist raw kubectl output, kubeconfig, DSN, or Secret values. Preserve every earlier process final across rotations and classify errors with existing FAIL precedence.
- [ ] **Step 4: Run GREEN:** `python3 -m unittest discover -s test-assets/traffic -p 'test_*.py'`. Expected: all traffic fixtures pass. Verify malformed/late boundary and missing process scenarios report INCONCLUSIVE, not PASS.
- [ ] **Step 5: Commit** worker collector, finish helper, fixtures, and README with `feat: close exact client traffic window`.

### Task 3: GitOps client Pod and operator runbook

**Files:**
- Modify: `helm-chart/infra-test-client/templates/deployment.yaml`, `tests/test_test_env_charts.py`, `helm-chart/infra-test-client/README.md`, `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md`

**Interfaces:**
- The Pod supplies `TEST_POD_UID` from `metadata.uid`, mounts a memory-backed `emptyDir` at `/run/traffic-control`, and runs as the existing UID/GID 65532. No new service, port, service-account token, or RBAC permission is added.
- The runbook names the exact worker boundary/finish commands from Task 2, the expected Pod/process roster, manifest/receipt checks, two-minute automatic restoration, and the action-time authorization gate. A disabled-mode path proof is explicitly separate from full replacement acceptance.

- [ ] **Step 1: Write a RED chart test** asserting the downward-API UID, memory-backed writable socket volume, immutable image, two replicas, existing probes, and no new RBAC/network object.
- [ ] **Step 2: Run RED:** `python3 -m unittest discover -s tests -p test_test_env_charts.py`. Expected: the new socket/UID assertions fail.
- [ ] **Step 3: Add the chart fields and update the runbook** with the reviewed command and stop sequence. Correct its inaccurate Ready-only Pod-discovery wording. Keep full execution blocked until Task 4's verified disabled-mode path proof and later action-time gates.
- [ ] **Step 4: Run GREEN:** `python3 -m unittest discover -s tests -p test_test_env_charts.py`; `helm template rdev-test-client helm-chart/infra-test-client -f infra-kubernetes/environments/rdev.ali/test/client-values.yaml`; affected Go/Python/Helm CI checks. Expected: valid render and all available focused tests pass; report missing dependencies separately.
- [ ] **Step 5: Commit** chart, tests, and runbook with `docs: wire reviewed client finish path`.

### Task 4: Review and disabled-mode release gate

**Files:**
- Modify only if evidence demands it: `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md`
- Retain sanitized private receipts in the existing encrypted evidence path only after separately authorized runtime access.

**Interfaces:** Reviewed Tasks 1–3 are prerequisites. The immutable image receipt and GitOps pin must name the same source commit. The live Pod spec must match Task 3; the Gateway flag must read `false` before and after proof.

- [ ] **Step 1: Obtain an independent whole-change review** of exact identity, lifecycle, five-second close, provider/traffic failure precedence, chart security and runbook; fix and re-review blocking findings.
- [ ] **Step 2: Verify offline gates**: Go race/vet, full traffic fixtures, chart tests/render, GitOps validation, and CI image build. Record exact commands/results and any unavailable dependency; no blanket PASS from skipped checks.
- [ ] **Step 3: Prepare a reviewable release** through the existing PR → image receipt → immutable digest pin → Argo flow. Do not provision Alibaba resources or deploy an unreviewed image. Confirm current cluster/operator access, cost/time gate, and `replacementEnabled=false` before any live action.
- [ ] **Step 4: At the action-time gate**, present the exact worker/kubeconfig transfer, target Pods and process IDs, commands, two-minute hold, receipt location and cleanup. Wait for specific authorization before transfer or disabled-mode finish. Then run one short disabled-mode capture, inspect both finals/manifest/assessment and verify automatic 2/2 restoration with healthy SQL/metrics. If access or approval is unavailable, record the concrete blocker and stop safely.
- [ ] **Step 5: Update the runbook** with observed source/image/skill SHA, evidence result and cleanup receipt. Keep the full replacement blocked unless the original live-test preflight and approvals are separately satisfied; never label the disabled proof as replacement PASS.
