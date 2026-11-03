# PgCat client metadata batching amendment

Owner approved this bounded amendment on2026-11-03 after reviewing the measured proposal. This supersedes the Gateway-only concurrency restriction in the action-observation spec solely for the three client metadata reads described below. All other contracts in that spec remain binding. Production scope is at most the existing NativeController and NativeClosure modules, plus existing tests; the27-key registry and cumulative15 production paths remain unchanged.

## Purpose and measured limit

The reviewed085d source accepts actual Gateway init metadata. A fresh-client read-only restart proxy took4.284609s, including two deliberately extra cold snapshots totaling0.344076s. After accounting for the remaining identity checks, two adapter constructions, historical evidence replay, prepared/credential reads, transport and an illustrative0.120s write/receipt allowance, planning estimates are4.96s using the median measured identity-check cost and5.05s using the slowest observed sample. These are estimates, not bounds or runtime qualification. The five-second full-action margin is inadequate; no new proof is justified from the proxy alone.

## Exact change

At the beginning of `FixedWorkerReads.live_clients`, fetch only these already-existing independent reads concurrently:

- named Deployment `rdev-test-client-client` in `raptor-test`;
- ReplicaSets selected by `app.kubernetes.io/name=rdev-test-client-client` in `raptor-test`;
- Pods selected by the same label in `raptor-test`.

Preserve exact existing argv/selector/name, credential checks, response projections and every existing ownership, generation, image, runtime identity, starting-Pod and roster validator. Decode all three successful bounded responses before accepting a metadata group. Return the same Deployment object, ReplicaSet items and Pod items to the unchanged remaining live-client logic. Missing, malformed, foreign or incomplete evidence must still refuse.

Use the existing main-thread owned-child pattern: retain all actual Popen handles, cap each stdout at2MiB, use one original absolute `reads.deadline` for launch/communication/completion and keep `--request-timeout=2s`. On failure, timeout or interruption, kill/wait/close all owned siblings before returning failure. No unbounded thread executor cleanup, partial accepted result, retry or renewed deadline. A shared private transport helper may serve the two fixed Gateway/client contexts; it must internally permit only those exact fixed argv forms, not expose a general shell/command list or caller-controlled command runner. Preserve existing Gateway before/after source/binary/prepared/credential checks and all its consumers.

Both action gates remain sequential. Pod `/evidence` HTTP reads remain sequential. Gateway config reads remain sequential. Runtime checks, two warm identity snapshots, actual cold inspection, final authority checks, PATCH and readback remain ordered as before. No new data cache or observation reuse. Existing action-only issuance and terminal invalidation are unchanged.

## Constraints and verification

No new API, resources, IAM, credential permissions, dependency, controller, source key, image pin or production path. Preserve5s action/3.5s false-idle worker/4s owned call/3s frame/15s metrics/30s provider-runtime, capture/end/lease and all approval/connection/cleanup gates. Replacement remains false until a real closed disabled proof authorizes the subsequent full2→4→2 flow.

Write meaningful RED tests before implementation for actual overlap of only the three fixed child commands; output caps/malformed or missing data/failing or slow child/interruption must leave no accepted result and reap all actual children. Verify original deadline and credential drift refusal, exact selectors/names, unchanged projections and existing ownership/rollout/starting-client behavior. Existing Gateway batching and source/action reuse tests must remain green. Cover shared-helper cleanup behavior with actual disposable children rather than only favorable dictionaries.

Run focused normal/optimized checks while iterating, required full traffic suite at final source,27-source Python3.6 grammar/import checks and exact freeze/scope checks. Independent task and final review plus exact-head CI precede merge. Root then stages frozen bytes, runs the CPU/process-only selection on actual worker Python3.6, and repeats the same genuine read-only composition with explicit transport, source/prepared/replay and conservative write/receipt allowance. Do not fake native eligibility, a plan, ready, Journal or runtime timestamp to obtain a timing result.

Only demonstrated adequate full-action margin and fresh false/idle fit may release one new source-owned disabled proof. Its real assessment,120s hold, restoration, both credential absences, global audit and qualification must pass before replacement. If margin still fails, return that measured result; no automatic additional scope or deadline widening.
