# Bind reconciliation to actual scale-in evidence

This is the local admission unit of existing Task 6D, following the reviewed exact-target worker. Keep public paid release guards closed. Requests retain their existing exact schema; no operator-supplied ACK, command, address, path or worker context is accepted.

## Actual evidence and admission

When a same-run `scalein-three-to-two` request completes successfully, seal one `scalein_ack` proof in the same terminal transaction. It binds local request UUID, run/session, admitted sequence, commit wall time (`sealedAt`) and sanitized provider RequestId. A controller state string cannot substitute for this record.

`Journal.reconcile_context(receipt)` derives the worker's exact six fields from that actual ACK, immutable Pending binding and current receipt: feedSession, sequence, observedAt, scaleinRequestId (local UUID), scaleinAcknowledgedAt (sealedAt), pendingBindingSha256 (canonical compact ASCII summary digest). Require an active disabled-rehearsal run, same run/session/scope, a fresh desired-two receipt with exact protected-healthy original-roster digest, strictly higher sequence and observedAt after ACK. ACK and Pending are immutable provenance; do not require their old timestamps under30seconds. Current receipt age must be nonnegative and under3.5seconds. Shape/type/session/sequence/digest validation remains strict.

The getter is an observation, not permission. `Journal.admit` must rederive and validate this context from the actual admitted receipt inside the existing transaction, reserve semantic `(run,reconcile-close)` before dispatch, and persist an immutable `reconcile_admission` record containing request/run/context/receipt identity. A new UUID cannot replay it. The admitted context, not a later live receipt, is used to validate the result if the provider publishes again while the local response is being sealed.

## Fixed dispatch and result

Supervisor obtains Pending and reconciliation context from the journal and verified receipt, builds only the reviewed eight-argument phase-worker command, and passes that same receipt into journal admission. It retains source checks, its exact owned CLI child, absolute five-second budget, lock/one-use sequence and existing uncertainty handling. No separately launched operator CLI or alternate transport is introduced.

Catalog maps existing action `reconcile-close` to the fixed private worker verb. It checks exact context keys, canonical identifiers/digest and consistency with run/Pending. It accepts the existing two JSON rows and validates exact same-session/admitted sequence plus canonical positive decimal resourceVersion (at most32digits). No caller worker parameters enter the request.

On successful completion, `Journal.seal(..., reconcile_source_sha=<reviewed rehearsalContract hash>)` compares actual run source hash and the immutable admission to the response, then stores one address-free `reconcile_binding`. Bind request/run/session/sequence/resourceVersion, actual scale-in request, Pending digest and source hash. Reject extra source arguments on other actions, missing admission, changed source/context/result, repeated seal or late/cancelled state. Preserve standard complete/uncertain transactional semantics. Getter `reconcile_binding()` supplies later feed-stop/finalization provenance.

## Controller and provider continuity

`Controller.reconcile_rehearsal()` runs only from `scaled-two` with a live exact feed and fresh exact-original desired-two receipt. Use internal owned `_worker_call('reconcile-close')`; compare the three-field payload to the sealed journal binding and then require a strictly later successful same-session desired-two publication observed after the call. Only then store the proof and enter `reconciled-two`. Include that state in liveness checks. Duplicate invocation or any uncertainty refuses; do not stop the feed yet.

The provider currently retains its three learned monitoring addresses even after members return to two. Its cached unchanged target tuple means the next successful tick does not rewrite the explicitly reconciled ConfigMap. Add a fake regression proving this exact sequence; do not clear retained history or modify provider production without a demonstrated defect. Any target drift still fails closed in the existing path.

## Verification and boundary

Fake tests cover ACK absence/wrong run/session/source, old or changed receipt/roster, semantic duplicate under a new UUID, admission/response races, exact argv/parsing, missing/changed result provenance, desired-two successor and retained-cache behavior. Preserve all no-scale public contracts and release rules. Run focused/full normal and optimized suites sequentially, Python3.6 grammar and independent review. Record existing short-deadline fixture variability honestly; a clean full normal release gate is still required before whole paid release. This unit changes no credentials, resources, deployment or live state. Paid feed stop, post-stop finish, verdict, abort, cutoff/cleanup and final whole-graph review remain downstream.
