# PgCat target readback checks under optimized Python

## Observed defect and intent

The provider feed's worker-side `_TARGET_SCRIPT` checks kubeconfig file mode, exact ConfigMap name/namespace, presence of `targets.json` and post-patch exact readback with `assert`. Python `-O` removes those checks. An existing rehearsal-mode regression that expects a failed incomplete readback passes normally but fails under `-O`. This was discovered offline after the 2026-10-26 second rehearsal; it is not evidence that the worker ran optimized Python during that failure.

The goal is to make those existing checks unconditional so an incomplete or wrong readback still stops publication under either interpreter mode. Preserve the 15-second absolute provider deadline, resourceVersion-bound patch, no write retry, and unchanged ConfigMap scope. Do not perform a live target change for this fix.

## Approaches

1. Require operators never to use `-O` or `PYTHONOPTIMIZE`. Reject: safety checks should not depend on interpreter launch flags.
2. **Selected:** replace only `_TARGET_SCRIPT` guard `assert` statements with explicit `if not ...: raise ValueError(...)` categories. Update the in-memory test boundary's own `assert` to an explicit failure so the optimized test is meaningful. Add an optimized subprocess regression that executes the real embedded script against the fake kubectl boundary. No new API, workload, credential or cloud resource.
3. Refactor the whole provider feed. Defer: a broad rewrite adds risk without addressing more of this defect.

## Verification and gate

- Test first: reproduce the missing-readback acceptance under `python3 -O`, then assert it refuses in both normal and optimized modes. Verify wrong ConfigMap identity, missing `targets.json`, bad kubeconfig mode and mismatched final target set also refuse under `-O`.
- Run focused provider/rehearsal tests and the full traffic suite normally and under `-O`. Preserve fixed sanitized error categories; no raw kubeconfig or provider output in test logs.
- Independently review the diff, then merge the source-only fix through the existing PR flow before a new live proof. The full replacement remains blocked by the separate access-lane/controller/no-scale rehearsal gates.
