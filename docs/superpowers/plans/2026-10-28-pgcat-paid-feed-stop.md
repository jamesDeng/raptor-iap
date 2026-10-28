# PgCat Paid Feed Stop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Bind the paid feed before startup and prove one exact graceful stop after reconciliation.

**Architecture:** Reuse the inherited-pipe barrier and actual Popen exit confirmation. The provider acknowledges a distinct reconciliation-bound stop; the journal independently reads durable records before exposing a feed-stopped proof. Public paid runtime remains disabled and no finalization is supplied yet.

**Tech Stack:** Local Python3.9, Python3.6-compatible traffic syntax, private JSON/SQLite, fake provider/process fixtures.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-paid-feed-stop-design.md`

## Global Constraints

- Existing checkout/branch only; no cloud/network/real credentials/billing/Raptor/staging/deployment in implementation.
- Keep production `_release_admission`, NO_SCALE_ACTIONS and request keys unchanged. No new source map or enable flag.
- No-scale exact180seconds/target argv and stop protocol unchanged.
- Paid cap canonical75..5400, no more than remaining time to22:30 minus120seconds; paid launch before21:30, provider interval15seconds.
- One pipe-gated actual child, one binding, one actual exit confirmation; no numeric PID adoption or caller status proof.
- One reconciliation-bound intent/ack, at most one further publication,30-second stop wait and final receipt age<30.
- No normal worker call or successor after feed exit; no assessment/finished/cleanup PASS in this unit.

## Review Focus

- Failed journal binding must prevent provider execution in either mode (Task1).
- Natural cap exit0 without actual acknowledgement cannot pass stop (Task1).
- A publication races the intent: at most one later exact-two sequence, not synthesized timestamps (Task1).
- Direct journal stop call cannot trust supplied PID/status/dict or adopt unrelated receipt paths (Task1).
- Paid close leaves owner alive but normal worker calls refuse after feed-stopped (Task1).

### Task 1: Paid startup barrier and graceful stop proof

**Files:** Modify `test-assets/traffic/pgcat_lane_client.py`, `pgcat_lane_protocol.py`, `provider_feed.py`, `pgcat_rehearsal_controller.py`; matching `test_pgcat_lane_client.py`, `test_pgcat_lane_protocol.py`, `test_provider_feed.py`, `test_pgcat_rehearsal_controller.py`. No other source without reporting a concrete dependency; in particular no finalizer/supervisor-finish/worker/source-map changes in this unit.

**Interfaces:** Generalize `start_feed`/`confirm_feed_exit` and `Journal.bind_feed(binding, *, manifest_raw=None)` to the two modes with unchanged actual-child guarantees and no-scale argv. Paid bind atomically retains the exact original outer manifest bytes and SHA/root/inner-binding comparisons; no-scale may omit it. Use this immutable record for stop receipt-path provenance. Route both controller modes through start_feed. Add exact paid stop constants/validators and provider immutable-mode callback, canonical stop acknowledgement of actual final receipt. Add no-argument `Journal.record_rehearsal_stop()` and getter `rehearsal_stop()` deriving private bound artifacts plus independent binding/exit/reconcile proof. Add `Controller.stop_rehearsal()` from reconciled-two, actual30-second wait/stop validation/confirmation/record, store its proof and enter feed-stopped. Return feed-exit fields matching existing exact receipt schema, no caller assessment. Keep owner alive and refuse duplicate/normal post-exit actions.

- [ ] Add RED tests for paid exact argv/cap/cutoff and inherited barrier order/EOF/failure; same-handle exit; ordinary no-scale invariance; correct paid stop, in-tick sequence race, missing/malformed ACK, natural cap, duplicates, stale/changed final receipt, wrong proof/session/source/sequence; direct journal record missing binding/exit/original manifest or tampered original/current bindings and fixed-path records; controller wrong state, uncertainty and no normal call after stop.
- [ ] Run focused fixture modules and record expected feature RED. Use fake CLI/process/clock boundaries and existing actual-child marker fixtures; no long waits or real provider calls.
- [ ] Implement only the bound launch/stop/proof lifecycle. Acknowledge actual final data, never fabricate a fresh observation. Use private O_EXCL/fsync records and strict canonical data. Keep global paid release closed.
- [ ] Run focused normal and optimized, full normal then optimized sequentially, changed-source Python3.6 grammar and diff checks. Diagnose/report bounded short-deadline transients; no unbounded retries/widening.
- [ ] Self-review, commit only owned files, report exact RED/GREEN/evidence/limitations and obtain independent scoped review. This is not full paid closure or live clearance.

## Downstream

A distinct fixed post-stop validation/finish action must use the confirmed final receipt under30seconds, not the ordinary one-second live-receipt wrapper rule or a successor wait. The guardian needs exact owned abort and retained failure evidence. Paid local credential identity/cleanup, ordinary whole-window verdict, independent supervisor finish, cutoff/recovery ownership, whole-graph review/CI/source staging and fresh no-scale proof remain required before paid admission can change.
