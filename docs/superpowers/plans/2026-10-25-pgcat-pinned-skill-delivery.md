# PgCat Pinned Skill Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the immutable PgCat replacement skill visible to the locked-down Pi model before any tool call.

**Architecture:** Read exactly three files from the already verified staged skill path into the replacement system prompt. Fail closed on missing, nonregular, symlinked or oversized content; keep the tool list and qualification path unchanged.

**Tech Stack:** Node.js ESM, Pi 1.3.0, Node test runner, existing Gateway/AX release and GitOps pin flow.

**Spec:** `docs/superpowers/specs/2026-10-25-pgcat-pinned-skill-delivery-design.md`

## Global Constraints

- Preserve pinned `skills-v0.4.0`, exact request binding, no `read`/`bash` tools, and replacement approval gate.
- Keep cloud flag false throughout code/test/release preparation; cloud provisioning remains Terraform/GitOps owned.
- Stop paid attempts when fresh whole-POC net forecast plus reserve exceeds CNY 1,500.

## Review Focus

- Missing or replaced reference file must fail before inference: Task 1 test.
- Symlink in selected path must fail: Task 1 test.
- Prompt delivery must work with no `read`/`bash` tool: Task 1 integration test.
- Qualification prompt must stay unchanged: Task 1 regression test.
- Resume must use the same pinned content: Task 1 integration test.

---

### Task 1: Deliver the staged pinned skill

**Files:** Modify `components/agent-harness/live-replacement.mjs`, `components/agent-harness/live-replacement.test.mjs`, `components/agent-harness/live-qualification.test.mjs`, `components/agent-gateway/Dockerfile`, and `components/agent-gateway/internal/runtime/live.go`; create `components/agent-harness/pinned-replacement-skill.mjs` and its test.

**Interfaces:** `pinnedReplacementInstructions(skills): string` consumes the existing verified `{tag,commitSHA,path}` staging result and produces the bounded prompt section or throws `InvalidSkillsRelease`.

- [ ] Write failing tests for actual Pi prompt visibility with no file tools, missing/reference/symlink refusal, qualification unchanged and resume content continuity.
- [ ] Run focused Node tests; confirm the visibility test fails for absent instructions.
- [ ] Implement explicit three-file bounded loading and append it only to the replacement system prompt.
- [ ] Add the new module to both image and AX guest transfer allowlists; run `python3 -m unittest scripts.rdev.test_qualification_packaging` to prove import closure.
- [ ] Run focused and full harness tests, then affected Gateway tests and source formatting checks.
- [ ] Review the diff and commit only the intended files.

### Task 2: Reviewed release and preflight

**Files:** Existing release PR and GitOps pin files; update the project handoff with actual observations.

**Interfaces:** Immutable Gateway and AX Pi guest image receipts from merged main to their separate GitOps pins; no mutable tag. The AX guest runs a baked `/opt/raptor-harness`, so a Gateway-only rollout cannot deliver this fix.

- [ ] Open and review a draft source PR; satisfy CI and merge under the owner's standing POC authorization.
- [ ] Publish the new Gateway and AX Pi guest images from the merged source; verify both immutable digest receipts and that the AX guest contains the new harness module.
- [ ] Pin both digests through reviewed GitOps changes (`infra-kubernetes/environments/rdev.ali/values.yaml` for Gateway and `infra-kubernetes/environments/rdev.ali/ecs-ax/bridge-release.json` for AX Pi); verify both rollouts, idle Gateway and `replacementEnabled=false` before another replacement attempt.
- [ ] Run fresh qualification, cost, fleet and traffic preflight before another full request; retain evidence and cleanup after any run.
