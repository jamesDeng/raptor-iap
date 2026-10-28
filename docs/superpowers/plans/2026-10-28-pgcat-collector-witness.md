# PgCat Collector Witness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax for tracking.

**Goal:** Produce bounded source/process/frame-bound collector evidence for the later post-stop finish.

**Architecture:** Existing source-verified disabled collector supplies an optional private context. Segmented capture retains exact durable tail references and sticky progress; worker capture seals its own actual settling/final boundary before heavy summary work. No paid execution capability is enabled.

**Tech Stack:** Python stdlib, Python3.6-compatible worker source, private fsynced JSON and deterministic fixtures.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-collector-witness-design.md`

## Global Constraints

- Existing linked checkout/branch only; no cloud/network/credentials/Raptor/provisioning/staging during implementation.
- Keep `_release_admission`, both NO_SCALE_ACTIONS, request fields, source-map keys, no-scale argv/outputs and timing bounds unchanged.
- No new CLI switch, generic helper capability, full-window scan or segment hashing on the bounded progress/boundary path.
- Match the actual collector child tuple, source hashes and original immutable context; never caller PID/status proof.
- Frames remain durably written before their references; private files0600/directories0700, no symlinks, bounded reads and duplicate-field rejection.
- Five-second settling clock stays collector-owned; final seals precede heavy assessment; ordinary full archive remains sole PASS.

## Review Focus

- A concurrently replaced segment/progress inode or partial final line cannot become trusted evidence (Task1).
- Filtered persisted events must be hashed as actual bytes, not reconstructed source input (Task1).
- Earlier failed poll, SQL failure, gap or identity change stays visible after later clean frames (Task1).
- Collector starts before guardian identity exists: bounded match/refusal, no PID adoption (Task1).
- Slow archive summary cannot delay the durable final-boundary seal or extend settling (Task1).

### Task 1: Exact bounded progress and boundary witnesses

**Files:** Modify `test-assets/traffic/segmented_capture.py`, `worker_capture.py`, `pgcat_collector_control.py`; matching `test_segmented_capture.py`, `test_worker_capture.py`, `test_pgcat_collector_control.py`. No other production sources without concrete dependency report.

**Interfaces:** Optional private `progress_context` on run_capture/main; optional context on SegmentedCapture; bounded `read_progress(directory, expected_context)` validates actual tailbytes and returns sanitized progress plus its bounded frame pair. Collector-control builds context only for verified disabled capture and matches actual child identity. The spec's exact context/binding/FrameRef/progress/boundary-start.json/boundary-completed.json schemas pin record fields. `read_progress(directory, expected_context)` returns validated progress plus decoded exact previous/latest frames. Other private witness helper names may be chosen consistently; schemas and public signatures are pinned by the matching design; report any unavoidable ambiguity before implementing a different authority model. Worker capture notes poll errors and writes exact own-clock start/completion seals without changing existing request/ACK/receipt shapes.

- [ ] Add focused failing tests for real-file byte ranges/hashes/newlines/rollover/filtered events, context/inode/source/process mismatch, sticky counters/error/gap/identity/skipped, initial poll error, replaced/symlink/truncated/duplicate-field artifacts, exact final markers/receipt/counters and boundary seal before slow finish. Assert unchanged default no-scale artifacts/argv.
- [ ] Run RED fixtures and retain relevant failure output with feature-related cause.
- [ ] Implement only fixed witness production/validation and verified private context injection. Keep caller-visible admission and release paths unchanged. No unbounded retry or timeout widening.
- [ ] Run focused suites normal/optimized, then full traffic normal/optimized sequentially once final code is ready. Check changed-source Python3.6 grammar, protected guards/source maps, diff hygiene; diagnose failures without retries to green.
- [ ] Self-review, commit only owned files, report RED/GREEN and exact schemas/interfaces/limits, then independent task review. This is not paid closure or live qualification.
