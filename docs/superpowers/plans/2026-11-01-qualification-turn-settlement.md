# Qualification Turn Settlement Implementation Plan

> For agentic workers: use superpowers:subagent-driven-development.

**Goal:** Preserve read-only qualification pause/resume when the model splits initial reads, within ten model turns.
**Architecture:** Minimal qualification-only lifecycle correction; existing MCP/proof/evidence/approval controls unchanged.
**Tech stack:** Node ESM, pinned Pi1.3.
**Spec:** docs/superpowers/specs/2026-11-01-qualification-turn-settlement-design.md

## Constraints
10 turns,90 seconds,1024 output; replacement disabled; no public API/resource/IAM changes. Preserve actual Raptor pause acknowledgement and full final evidence verification.

## Task1
- [x] Reproduce actual first three separate read rounds then remaining planned batches, both probes, approval, pause with pinned Pi. Record RED and exact cause.
- [x] Implement smallest qualification-only settlement preserving protocol and no extra model round. Verify split initial and resumed reads, errors/missing/stale evidence, failed approval/pause, explicit pause compatibility and unchanged replacement.
- [x] Run focused and full harness suites; commit only exact task files.
- [ ] Independent review by the coordinating agent.

## Release
- [ ] Reviewed PR/CI/merge under standing authority. Publish immutable image and GitOps pins for every actual harness consumer; verify hashes/idle/disabled.
- [ ] One new actual qualification only after all gates; inspect native approval/pause/resume/fresh evidence and cleanup. No PASS until all checks.

## Local reproduction and verification

- RED: pinned Pi 1.3.0 ran ten successful model calls with split request/environment/object reads; approval completed, `request_pause` was never called, and the next wrapper `TurnLimit` became an SDK assistant error reported as `ModelFailed`.
- A second RED showed that an `accepted: false` pause response could settle locally; qualification now requires the actual Raptor success acknowledgement.
- The focused pinned SDK tests cover batched/split reads, explicit pause compatibility, exact persisted pause identity and acknowledgement, normal tool progress, failed/malformed/unacknowledged pause, failed or mismatched approval, incomplete probes, sibling errors, denied continuation, stale evidence and provider errors.
- Both initial and resumed split sequences use exactly ten provider calls and ten usage records. The complete resumed segment ends at the SQL round; no final assistant inference is needed.
- Only `components/agent-harness/live-replacement.mjs` changes production behavior. The active AX guest requires a new immutable image; the active Gateway observer import graph does not contain that file.
- Focused verification: `node --test live-qualification.test.mjs` from `components/agent-harness`: 16/16 passed.
- Full verification: `RAPTOR_TEST_PYTHON=/Users/dengwei/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 node --test components/agent-harness/*.test.mjs tests/harness/*.test.mjs`: 407/407 passed on Node 26.7.0 with pinned Pi 1.3.0.
- The first expanded run used macOS Python 3.9.6 and failed the unrelated existing `Python and Node canonical hashes and code-point limits agree` test because that Python cannot evaluate `str | None`. The supported bundled Python rerun passed without source changes.
