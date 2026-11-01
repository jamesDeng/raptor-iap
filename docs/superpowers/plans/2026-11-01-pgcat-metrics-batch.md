# PgCat metrics batch implementation plan

> Use superpowers:subagent-driven-development for the one bounded implementation task and independent review.

**Goal:** Complete all fixed PgCat metrics reads within the unchanged Gateway deadline.
**Architecture:** One observation-local concurrent eight-series acquisition on the initialized MCP session; existing validators consume its raw rows. No cross-observation cache or interface change.
**Tech stack:** Existing Node.js harness, Pi MCP client, node:test.
**Spec:** docs/superpowers/specs/2026-11-01-pgcat-metrics-batch-design.md

## Constraints
Existing checkout only; preserve unrelated untracked files. No cloud operations by implementer/reviewer, no credentials. Production scope only gateway-observation-runner.mjs plus focused tests. Do not widen deadlines, reduce freshness/coverage or change qualification approval/ordering. Existing live Gateway and AX image release processes required after review.

## Task1 — observation-local fixed-series acquisition
- [ ] Add deterministic deadline/barrier regression proving all8fixed series start without waiting for one another and a single initialized session; observe current code fail.
- [ ] Implement bounded concurrent batch scoped to a single observation, preserving raw rows, per-series validation and all existing guard logic.
- [ ] Add failure/cancellation and isolation coverage as needed; run affected harness suites, including metric freshness and qualification read sequencing.
- [ ] Self-review, commit only task files, report RED/GREEN evidence and precise change.
- [ ] Independent task and whole-diff review, fix material findings, verify and release via reviewed image/GitOps path.

## Runtime closure (controller)
- [ ] Verify failed1792 cleanup and remove both exact temporary operator contexts.
- [ ] Deploy reviewed immutable images only where changed harness executes. Verify readiness and unchanged replacement=false/client2/2.
- [ ] One read-only qualification retry, inspect actual baseline, harmless approval, pause/resume and fresh reads; verify final cleanup before PASS. Stop and diagnose any new failure without duplicate Continue.
