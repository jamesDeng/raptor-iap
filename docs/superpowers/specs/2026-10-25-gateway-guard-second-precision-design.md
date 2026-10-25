# Gateway rollout guard: Kubernetes second precision

## Intent and evidence

The guarded PgCat test must confirm that the replacement flag is loaded by a new Ready Gateway Pod. On 2026-10-25, the disable operation set the exact Secret to false at `05:02:49.080107235Z`, then restarted the Deployment at `05:02:49.546953035Z`. The new Ready Pod's `status.startTime` was `05:02:49Z`. The guard rejected it because `readyPod` requires the whole-second Pod timestamp to be strictly later than the subsecond Secret timestamp. Independent Secret, Pod, loaded-config, and idle reads confirmed false. The guard result remains INCONCLUSIVE until corrected and reverified.

## Approaches

1. Wait for the next second before restarting. This avoids the comparison failure but adds a timing dependency to the guarded operation and can still fail under clock skew.
2. Compare Pod start time at its reported one-second precision, while requiring the Deployment's restart annotation to be strictly after the Secret change, and retain the existing exact Secret version, ReplicaSet ownership, image/config binding, Ready, in-Pod loaded config, and final recheck. **Selected**: it preserves the intended proof across the two clocks without treating a same-second old Pod as sufficient by timestamp alone.
3. Remove the start-time check entirely. This weakens the rollout proof and is rejected.

## Scope and checks

Change only the local guard verifier. A Pod start reported at whole-second precision is accepted only when the rollout annotation is after the Secret change and its start second is at or after the annotation's whole second. A Pod start reported with fractional precision must be strictly after the annotation at full precision. The existing exact binding checks remain mandatory. Earlier Pods, absent or stale rollout annotations, stale Secret versions, foreign owners, wrong loaded config, and concurrent updates still fail closed. Add focused regression cases for same-second success, stale-annotation rejection, and a premature fractional Pod start, then run the full guard package tests. This design makes no cloud change by itself.
