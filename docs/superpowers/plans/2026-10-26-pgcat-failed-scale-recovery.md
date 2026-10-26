# PgCat Failed Scale Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the two protected original PgCat nodes after failed request `570533ed-e417-4bc5-beba-ad4db3db8d20` left desired capacity four.

**Architecture:** This is a serial, one-operator runtime recovery using the existing ACK Workbench CLI and Alibaba ESS/NLB APIs. It makes no Terraform or GitOps desired-state change and cannot upgrade the failed capture to PASS. A changed or uncertain observation stops the sequence without replay.

**Tech Stack:** Existing Workbench CLI, ACK `kubectl`, Alibaba ESS/NLB CLI, existing private Prometheus and test-client observations.

**Spec:** `docs/superpowers/specs/2026-10-26-pgcat-failed-scale-recovery-design.md`

## Global Constraints

- Exact environment `rdev.ali`, proxy `DP00001`, ESS group `asg-t4necb4dfmu6o3z2rn0g`, NLB server group `sgp-xs5s42tihtl6ja2a67`, ACK cluster `c92787e953503492ea141a744c81498f1`.
- Originals `i-t4n8ofwrovy3mv198ela` and `i-t4n8ofwrovy3mv198elb` remain Protected, healthy and registered.
- Only new nodes `i-t4n237xiu1dr1lkxfypk` then `i-t4n237xiu1dr1lkxfypl` may leave.
- Keep `replacementEnabled=false`, no new replacement request and no new kubeconfig.
- Every mutation has one provider submission, no automatic retry; an unknown acknowledgement stops recovery.
- Do not print credentials, private addresses or raw Secret/ConfigMap content.

## Review Focus

- A changed member or protection state must stop before the next mutation.
- A deregistration whose job is only accepted must be read back as converged before draining.
- A partially restored client Deployment or SQL failure must stop before scale-in.
- Every active, idle and waiting series for the sole unprotected node must be fresh and zero across all pool/user labels.
- Provider acknowledgement and ESS convergence are separate; uncertain outcomes cannot be replayed.

---

### Task 1: Lock the observed state

- [ ] Recheck Raptor request terminal/cleanup, no pending approvals, Gateway flag false and `idle=true`, and no active AX actor.
- [ ] Read exact ESS/NLB group and four members. Require desired four, both originals Protected/Healthy/Normal, both new nodes InService/Healthy/Normal, listener running, and no foreign backend.
- [ ] Verify clients 2/2 Ready with current image and successful SQL, metrics Deployment 1/1 and complete fresh four-node pool triplets. Record sanitized IDs, times and provider request receipts.

### Task 2: Remove first new node (`...xfypk`) via 4→3

- [ ] Protect only `...xfypl` and verify three protected nodes, leaving `...xfypk` sole unprotected candidate.
- [ ] Deregister only `...xfypk`; verify job convergence and three healthy registered backends, strictly greater than 4/2.
- [ ] Restart only the exact `rdev-test-client-client` Deployment with UID/resourceVersion preconditions. Verify two restored Ready clients, advancing successful SQL and no adverse counters.
- [ ] Require fresh complete zero active, idle and waiting PgCat counts for `...xfypk` across every pool/user, plus full fleet/backend read.
- [ ] Submit one non-retried ESS `DesiredCapacity=3` change. Verify provider acknowledgement, then desired three, exact member departure and healthy registration for both originals plus `...xfypl`.

### Task 3: Remove second new node (`...xfypl`) via 3→2

- [ ] Unprotect only `...xfypl`; verify originals protected and `...xfypl` sole unprotected candidate.
- [ ] Deregister only `...xfypl`; verify job convergence and two healthy backends, strictly greater than 3/2.
- [ ] Restart the same exact client Deployment with a new UID/resourceVersion read. Verify two Ready clients, advancing successful SQL and no adverse counters.
- [ ] Require fresh complete zero active, idle and waiting counts for `...xfypl` across every pool/user, plus full fleet/backend read.
- [ ] Submit one non-retried ESS `DesiredCapacity=2` change. Verify acknowledgement and exact convergence to two protected originals, both Healthy/Normal and no new members.

### Task 4: Reconcile and settle

- [ ] With the prior collector/feed closed, reconcile only `raptor-test/pgcat-provider-inventory` to the verified actual fleet using its current resourceVersion; read back exact target IDs.
- [ ] Verify clients 2/2, advancing successful SQL, complete fresh metrics, Gateway false/idle, Raptor checkpoint/cleanup and no active actor.
- [ ] Preserve the already archived failed capture as INCONCLUSIVE. Delete both exact temporary kubeconfig copies only after no more ACK reads are needed; verify absence. Remove disposable run files only after retained evidence verification. Update the handoff with final state and unresolved gaps.
