# Lifecycle verification and review

One fresh GPT-6 Astra reviewer inspected the whole branch at `448c0b4` against the approved design/plan. The reviewer found no Critical issues and six Important issues. They were fixed in one regression-driven pass; no second reviewer was dispatched.

| Finding | Verified fix |
| --- | --- |
| Mounted Volume could differ from verified bucket | Check account/team/Volume identity, OSS class, bucket, prefix, endpoint, write mode and declared role before compute/key creation |
| Recovery or ledger failure could skip cleanup | Attempt bounded cleanup with known in-memory IDs despite result-read or persistence errors; preserve blocking intent and report persistence failure |
| Publication failure could change the selected reference later | Save a proposed ledger before mutating its selected checkpoint |
| Refresh/checkpoint success confused with inference completion | Only inference establishes request completion; completed tool/answer evidence survives checkpoint failure |
| Incomplete runner success accepted | Require phase-specific success evidence and a valid checkpoint for refresh/inference |
| Loaded ledger could leak nested values | Share strict result contracts and fixed error/outcome enums across fresh and loaded data |

Regression tests were observed failing before each fix. The real pinned Pi session test additionally exposed loss of turn-limit/timeout causes after abort; it now verifies fresh session context, three model-call limit, 1,024-token cap, disabled retries and the 90-second timer with only the provider stream replaced. Other added tests cover failed readback, uncertain-key reconciliation and recovery descriptor adoption.

Final full local verification: 73 Python tests and 8 Node tests passed. The existing Terraform provider fixture ran, rather than being skipped. Live evidence is in [the acceptance receipt](oss-sandbox-lifecycle-acceptance.json); destructive failure injection uses synthetic credentials offline.

The retained Volume has no `mount_config`. Its explicit role is supplied in sandbox create metadata, as verified by the SDK transport test and live run; a conflicting declared Volume role is rejected. The reviewer stayed credential-free. Separate author-run metadata/inventory checks support live evidence; this is not a second-party billing attestation.

Deferred readability improvement: expand semicolon-heavy code remaining in supporting modules. Distributed ownership, hosted scheduling, garbage collection, infrastructure operations, crash-safe continuous OAuth checkpoints and account-wide spending enforcement remain outside this slice. Their practical limits are in [the operator guide](oss-sandbox-lifecycle.md).
