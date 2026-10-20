# Raptor Approval Resume State Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans in the existing checkout. Do not create another project or checkout.

**Goal:** Make a saved approval immediately eligible for Gateway access renewal while keeping its request binding and human decision gate intact.

**Architecture:** In the approval transaction, change only a waiting request to queued and retire any undelivered pause signal for that request. Execution synchronization must ignore the old waiting snapshot for the same attempt until Gateway advances. Agent access continues to accept only queued or running requests.

**Tech Stack:** Go, PostgreSQL, existing Raptor outbox and execution view.

**Spec:** User-approved bounded design in this chat on 2026-10-20, following the observed qualification resume HTTP 409.

## Tasks

### 1. Approval transition

- [ ] Add database regression tests for approval from `waiting_approval`, old pause retirement, and unchanged denial behavior.
- [ ] Run focused tests and confirm the expected failure.
- [ ] Update the approval transaction with the conditional transition and pause retirement.
- [ ] Run focused tests to green.

### 2. Stale execution view

- [ ] Add a regression test showing an old `waiting_approval` view cannot overwrite an approved queued request.
- [ ] Run it red, add a narrow sync guard, and run it green.

### 3. Release gate

- [ ] Run Raptor tests and static checks on a local disposable database.
- [ ] Review the exact diff and CI; merge the reviewed fix under the user's standing authorization.
- [ ] Pin the immutable released Raptor image through GitOps and verify rollout with `replacementEnabled=false`.
- [ ] Retry a read-only approval resume before a fresh full two-node attempt.
