# PgCat Collector Supervisor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the exact detached rehearsal collector once, without an ignored SIGINT, PID reuse signal risk or inherited cloud credentials.

**Architecture:** The still-parent worker supervisor owns the collector `Popen` handle and a private FIFO. A one-use cancel caller writes a bounded identity-bound packet; only that supervisor signals its unreaped child, then seals a sanitized exit receipt. The capture child gets a minimal fixed environment and resets SIGINT before exec.

**Tech Stack:** Python 3.6 worker stdlib (`os.mkfifo`, `select`, `os.waitid`, `signal`, `subprocess`), Mac Python unittest, existing `worker_capture.py`.

**Spec:** `docs/superpowers/specs/2026-10-26-pgcat-workbench-access-lane-design.md`

## Global Constraints

- The earlier 465-frame rehearsal is INCONCLUSIVE; never relabel or splice it.
- No new cloud resource, credential path, ESS/NLB/Gateway/client/Raptor mutation or direct credential content in code/logs/receipts.
- The worker runs Python 3.6. The exact private run directory is mode 0700 and control files are mode 0600.
- A cancel uncertainty consumes its intent and permits no second signal. A process/exit receipt is not a traffic PASS.
- The active provider feed must be confirmed exited **before** a potentially long collector-cancel Workbench call; later access-lane integration must enforce this ordering.

## Review Focus

- `Popen.poll()` or `waitpid(WNOHANG)` would reap the child and permit PID reuse before signal: Task 1 test.
- The FIFO writer can disappear between intent and packet or submit twice: Task 1 test.
- A completed child may race a cancel packet: Task 1 test, with serialized admission closure before reaping.
- A retained environment variable may contain a cloud credential or proxy: Task 1 test of exact environment keys.
- An exit receipt may be confused with assessor PASS: Task 2 runbook gate.

---

### Task 1: Parent-owned one-use cancellation

**Files:** Modify `/private/tmp/pgcat-worker-collector-control-20261026.py`; test `/private/tmp/test_pgcat_worker_collector_control_20261026.py`. The current frozen source hash is `2b1eddc15a40315f8fb8074c689d0f6137aa4287e1509eea17f6580b9b9a76a0`; do not stage that revision to the worker.

**Interfaces:** `CollectorControl.launch()` starts one detached supervisor. The supervisor creates a mode-0600 FIFO in the private run directory and seals exact PID/boot/start/command identity. `CollectorControl.cancel(expected)` writes one durable intent, then one identity/nonce JSON packet smaller than `PIPE_BUF`; it never calls `os.kill`. The supervisor alone validates that packet against its held, unreaped child, sends at most one SIGINT and seals an acknowledgement. It uses a non-reaping `waitid(P_PID, pid, WEXITED|WNOHANG|WNOWAIT)` or equivalent while FIFO admission is open, closes admission before its final `wait()`, then seals `collector-exit.json` from the actual completion marker and exit code. The child receives exactly `PATH`, `HOME`, `LANG`, `NO_PROXY` and `KUBECONFIG` from fixed values.

- [ ] Add failing tests for duplicate packet, writer interruption after intent, changed identity, naturally exited child before/while packet arrives, unchanged unreaped PID during one signal, and exact child environment keys. Include one real local child whose parent ignores SIGINT to verify the actual child reset.
- [ ] Run `PYTHONPATH=/private/tmp PYTHONPYCACHEPREFIX=/private/tmp/pgcat-verify-pycache python3 -m unittest test_pgcat_worker_collector_control_20261026 -v`; capture expected RED failures before implementation.
- [ ] Implement the FIFO state machine in the existing scratch helper. Never call `poll()` or reaping `waitpid` before closing cancel admission; do not copy `os.environ` into the child. Keep all receipts fixed-category and owner-only.
- [ ] Run the test file normally and with `python3 -O`; compile for Python 3.6 on the exact worker only after a separate reviewed staging step. Freeze hashes and request independent code/safety review.

### Task 2: Bind controller and prove disabled no-scale stop

**Files:** Modify `/private/tmp/pgcat-rehearsal-controller-20261026.py`; test `/private/tmp/test_pgcat_rehearsal_controller_20261026.py`; update `docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md` after review.

**Interfaces:** The controller may request cancellation only after its owned provider feed's exit is sealed and verified. It passes the exact bound child identity to `cancel`, accepts only the supervisor's acknowledged one-use signal and later `cancelled` exit receipt, and never falls back to numeric PID/SIGTERM. The collector's partial archive remains INCONCLUSIVE unless independent assessment passes the whole window.

- [ ] Write failing tests that refuse cancel while feed is alive, refuse second cancel after uncertainty, and require exact identity plus sealed cancelled exit before cleanup.
- [ ] Implement only the ordering and receipt checks; retain the existing single feed/collector ownership and no retry of ESS/NLB mutations.
- [ ] Run controller and worker tests normally and with `-O`. Independently review frozen sources before staging.
- [ ] In a fresh replacement-disabled no-scale proof, verify one real supervised child stops once after verified feed exit, writes cancelled completion and sealed exit, and leaves two clients restored/healthy. No ESS/NLB/Gateway/Raptor mutation. If proof is uncertain, preserve evidence and stop without paid rehearsal.
