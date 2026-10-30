# Mode-bound PgCat rehearsal collector

This is the first bounded implementation unit of the already requested paid Task6D close/abort work. It implements the approved90-minute continuous capture lifecycle from the two-node live-test design, without enabling paid runtime. The user's direction to continue without repetitive approval questions supplies execution authorization; source review and fresh live gates remain mandatory.

## Contract

Keep the current no-scale collector's exact65-second capture at `run/collector-capture`, guardian80-second wait,90-second status bound and existing no-boundary argv. Its journaled timing assessment and public no-scale path remain unchanged.

Add a second fixed component profile `disabled-rehearsal`: output `run/rehearsal-capture`, inventory `run/provider-inventory.json`, duration5400seconds, boundary `run/disabled-boundary`. This matches baseline, Pending, pre-drain and zero-proof paths. The existing worker capture requires a boundary/finals to complete; reaching the duration cap without them is incomplete. Maximum segment duration remains1800seconds. The guardian waits5415seconds and then exact-child kill/reap remains bounded5seconds; status admits up to5430seconds from its sealed launch intent. These are component maxima, not authorization to exceed22:30; later controller integration must bind action-day deadlines and reserve cleanup before paid runtime can be enabled.

Expose only fixed private CLI verbs `rehearsal-launch`, `rehearsal-status`, `_rehearsal-run`, `_rehearsal-capture`; no caller-controlled command, path, duration or runtime capability flag. Python APIs may take keyword-only `profile='no-scale'`, accepting exactly the two fixed enum values. A paid intent seals `profile:'disabled-rehearsal'` before spawning; child/status/completion must read and compare it. No-scale intent remains its current exact schema, so one profile cannot adopt the other's job even with the same run UUID. Both profiles share one fresh collector-job directory, so duplicate launch under either profile refuses. Preserve the same exact Linux child identity, observed-ALIVE requirement, sealed exit and opened verified-source loader. All transitive source imports must execute reviewed snapshots, never caches.

This unit changes only the collector component and its tests. The existing public paid admission guard remains structurally closed in controller/client/protocol/feed/supervisor. No new paid action becomes reachable from the no-scale catalog. A later separately tested integration will add the profile's fixed lane actions, day cutoff, provider stop and post-stop finalization. This unit provides neither a live paid collector nor replacement PASS.

## Evidence and failure

A process exit or completion hash means only collector completion; ordinary whole-window assessment later decides PASS/FAIL/INCONCLUSIVE. Never suppress SQL faults, missing finals, gaps or metrics. Wrong profile, changed intent, missing boundary, missing/changed completion, identity mismatch, timeout and duplicate launch refuse. A timed-out owned child is reaped through its retained handle; numeric-PID rescue is forbidden.

## Verification

Add RED/GREEN tests for both exact argv profiles, immutable profile binding across detached child/status/completion, same-ID cross-profile refusal, one-use launch, actual long boundary capture completion versus missing finals/cap exhaustion, guardian/status budgets and timeout reaping. Exercise verified-source execution and Python3.6 grammar. Run focused and full traffic tests in normal and optimized modes. Independently review this component before catalog/controller integration. Re-freeze source and repeat a disabled proof after the full paid closure graph is complete; today's7508dfa timingPASS cannot authorize changed paid source.
