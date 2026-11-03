# Private startup failure diagnostics

Small operational diagnostic under the existing continuous debugging authorization; no new admission or evidence semantics. Original failed run remains INCONCLUSIVE.

## Intent and evidence
The780 disabled proof stopped before status request: only receipt2, empty startup result, two committed frames. Neither original provider nor guardian exception was retained. Do not guess a fix or relax a limit.

## Approach and constraints
Add failure-only private records in the two already in-scope modules: nativeController and collectorControl. At the controller call boundary distinguish waiting-for-receipt from dispatch; retain the actual feed return code observed BEFORE owner close, action, phase and fixed exception location/category. At the actual guardian startup boundary retain phase, actual elapsed/deadline state, child exit observed before cleanup, and fixed exception location/category. No provider source or feed protocol change; the original provider inner stage remains unavailable, and must be explicitly reported unknown. Independent read-only provider diagnosis may follow if the owner record identifies feed failure.

Records contain only fixed whitelist module/function/line or fixed category identifiers, numeric timing/return-code values, run/action identity. Never exception messages, locals, argv, URLs, credential values, HMAC keys or arbitrary paths. Private bounded JSON, fixed absent sibling names, owned0600/no-follow/exclusive write through pinned private directory; writer errors never replace original failure or impede child cleanup. Diagnostics never enter READY, qualification, admission, provider receipt or global audit and cannot grant authority. Emit only on failure, after owned child cleanup where appropriate; no additional success-path IO.

Alternatives: blind retry rejected for lost evidence; change timing rejected without cause; expanding the provider or lane-client scope rejected. Two-file diagnostics preserve existing cumulative15 production paths and27-source registry, Python3.6, all4/5/3/15/30 limits, min2/max4, desired>=minimum, sum(active+idle+waiting)<=1, approvals and SQL FAIL precedence. No new resources/IAM/API/dependency/processkind.

## Verification
TDD real failing boundaries with sentinel secret text, no-overwrite/symlink/error writer cases, original exception/cleanup preserved, exact feed returncode captured before owner kills it, actual guardian phase at failure. Focused normal/optimized, one full traffic suite,27 compatibility/hash freeze, task and final review, exact-headCI and actualworker-safe tests. Any new proof is diagnostic and fail-closed, never PASS from static tests.
