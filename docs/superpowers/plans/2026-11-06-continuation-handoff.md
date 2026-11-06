# Continuation handoff implementation plan

> **For agentic workers:** Use superpowers:executing-plans for inline implementation and a final independent review.

**Goal:** Remove browser-poll dependence from resource-wait continuation and safely recover the failed pre-runtime continuation in the same request.

**Architecture:** Raptor reconciles the exact preparing Gateway attempt before access issuance, with transactional control/freshness checks. Gateway accepts exact-attempt Continue only for cleaned failed preparation with validated saved session and resumed resource wait, without any runtime-start intent. Existing scope and approval checks remain.

**Tech Stack:** Go, PostgreSQL, existing HTTP APIs.

**Spec:** Approved parent-workspace working/handoffs/2026-11-06-pgcat-resume-design.md.

## Global constraints
No new replacement request or repeated expansion. Conversation/messaging and evidence freshness stay enabled. No private checkpoint downloads. User authorizes merge/deploy; exact scale-in still needs human approval.

## Review focus
Cancel/block races; wrong/stale Gateway attempt; scope drift; missing/corrupt saved session or runtime startup uncertainty; duplicate Continue affecting a later attempt.

### Task 1: Raptor handoff
Files: agentaccess/service.go, backend/service.go, requests/execution_view.go, new requests/attempt_handoff.go and backend/attempt_handoff_test.go.
- [ ] Test real-DB HTTP resource resume without browser polling, plus wrong/stale/offline/control rejection; observe failure.
- [ ] Add PrepareIssue callback to requests.PrepareAgentAttempt(ctx,id,attempt) error. Only waiting_resource needs synchronization. Validate exact live preparing attempt and guard sync transaction; preserve later access checks.
- [ ] Run full Raptor suite and commit.

### Task 2: Safe recovery
Files: requests/actions.go, execution/signals.go, new execution/preparation_retry.go and tests.
- [ ] Add failing exact-attempt recovery test and negative session, intent, cleanup and duplicate tests.
- [ ] Include exact failed attempt in Continue. Retain qualification behavior. Replacement recovery requires ProviderUnavailable, no runtime ID/events or non-access intent, validated immutable saved session and matching resumed resource wait. Preserve continuation.
- [ ] Run both suites, independent review, commit.

### Task 3: Deployment and qualification
- [ ] Merge after review/CI, publish and pin verified immutable images.
- [ ] Fresh preflight with mutation disabled; readonly qualification exercises automatic resource wait and actual message/reply.
- [ ] Resume original request at four nodes, fresh reads/traffic logs, no expansion replay, human exact scale-in approval.

## Ledger
User approved both repairs; native execution selected for coupled changes. Baseline 6fd8571. Prior real-DB HTTP diagnostic demonstrated same request 409 before View and 201 after. First test-writing command used wrong working directory and created no files; its no-tests result is not validation.

### Task 3 adjustment: readonly validation at the existing four-node fleet
The old qualification required exactly two protected nodes, so it cannot validate the held fleet without unnecessary mutations. Extend only readonly qualification v2 to stable exact two or four healthy backend members, probe every member, require capacity/set equality, and preserve original two-node protection requirement. Four-node qualification observes protection without changing it. Existing mutation guards are untouched. The skill now exercises resource_wait before readonly approval, yielding three segments. The actual Pi regression covers four-node resource checkpoint/resume followed by approval checkpoint/resume. Old v1 evidence remains exactly two nodes.

Ledger: full Raptor passed; initial full Gateway revealed the old replacement-Continue test expected silent no-op. Updated to expect explicit InvalidInput while retaining failed status. New four-node Pi and both result-validator tests failed first; Pi three-segment test now passes. Skill baseline agent correctly stopped at old two-node requirement; updated guidance validation pending. Full suites running again for final combined branch.
