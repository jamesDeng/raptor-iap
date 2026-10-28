# PgCat bounded collector witnesses

## Intent and scope

Supply durable bounded evidence for the next fixed post-stop finish without scanning or hashing a whole5400-second capture within its five-second RPC. This unit only produces and reads collector witnesses; it supplies no finish dispatch, paid admission, verdict upgrade, live proof or cloud operation. Existing no-scale outputs and admission remain unchanged.

The user requested continuous implementation/testing/review and opted out of repeated design questions. The controller chooses the existing Python/std-lib implementation and independently reviewed subagent workflow. The alternative of a full archive scan inside finalization violates the bounded RPC; relying on mutable summary booleans lacks actual frame-byte provenance.

## Chosen interface

`worker_capture.run_capture(..., *, progress_context=None)` and a private keyword on its `main` carry immutable context only from source-verified collector control for `_rehearsal-capture`; there is no new CLI switch or caller capability. Context binds canonical runId, the existing verified source hash map, and the actual collector child process tuple. The child must match the guardian-written `collector-job/identity.json` to its own current Linux tuple under a bounded startup wait. It must never substitute a guardian PID or adopt a caller numeric PID. The default no-scale invocation remains `module.main()`.

`SegmentedCapture` optionally receives this context. It creates an exclusive immutable binding containing its private capture directory identity. After each successful existing frame fsync, read only the actual appended byte range, including its newline, and retain its exact segment basename, offset, length, SHA256 and timestamp. Retain the previous reference and latest reference so later admission can read a bounded pair. Validate private ownership/modes, regular files, no symlinks, stable opened inode and byte length; no directory traversal or arbitrary path from JSON. Whole-segment hashing remains outside this path.

Publish atomic private fsynced progress with context/binding digest, committed frame count, first/last time, maximum gap, exact recent frame references and sticky observed poll/identity/SQL failure/skipped indicators. An error before a first successful frame must remain visible with committedFrames0; a later clean frame must not erase it. SQL recognition reuses the existing v2 `observed_sql_failure` contract; sticky observation is not a whole-window PASS. Identity covers the exact initial Pod/container/process/deployment identities. Ordinary archive assessment remains authoritative for complete transition, metrics, event/counter and continuity validation.

Only when progress_context is bound, write exclusive small boundary-start.json and boundary-completed.json seals from the collector's own monotonic boundary, bound to the exact request and original roster. Boundary complete is written only after both actual final markers, exact acknowledged receipt, settled counters and Connected0 within the existing five-second collector limit. Write it before synchronous segment hashing or assessment. Preserve request/ACK/receipt formats and original clock start; never restart the clock at ACK. Missing, mismatched, stale or incomplete witnesses refuse bounded admission. No new witness can convert ordinary INCONCLUSIVE/FAIL into PASS.

## Verification and boundaries

Use deterministic frame/clock fixtures and real private temporary files to prove actual byte binding, segment rollover, filtered events, tamper/partial line/symlink/replaced inode rejection, sticky error/gap/SQL/identity/skipped state, duplicate/exact boundary controls and final seal before blocked slow assessment. Prove default no-scale artifacts and existing argv unchanged. Run focused/full normal then optimized sequentially, Python3.6 grammar, source-map/guard checks and independent scoped review. Preserve unrelated iCloud files.

Later units must admit fixed post-stop dispatch, use conservative worker-local deadlines, consume these provisional witnesses, retain authenticated FAIL through partial completion, seal actual collector exits, perform ordinary detached assessment, verify actual2/2 restoration, remove exact temporary credentials and independently derive finish. All remain separate gates before live paid execution. Native observer and client session rotation also remain unimplemented readiness gates.

## Exact record and provenance contract

## Original context and provenance

The injected context has exactly these keys:

```text
schemaVersion: 1
runId: canonical UUID
feedSession: canonical UUID
providerRunContextSha256: SHA256 of exact private provider-source/run-context.json bytes
bundleSha256: original provider runContext.bundleSha256
sourceHashes: exact worker-source hash subset already verified by collector-control.context()
baselineSha256: SHA256 of exact seal-verified baseline.json bytes
originalRoster: original baseline.roster (two exact name/uid/containerId/processId objects)
deployment: original baseline.deployment mapping
collectorIdentity: {pid, bootId, startTicks, commandSha256}
```

`collectorIdentity` is the **actual capture child** tuple, the same tuple the guardian writes to `collector-job/identity.json`; do not accidentally supply the guardian process identity. In `_rehearsal-capture`, derive it with the existing `process_identity(os.getpid())`. Parent recording can race initial child execution: wait at most1s (fixed worker-local monotonic deadline, short sleeps) for the private identity record, then require exact equality with this self-derived tuple before context injection. Timeout/mismatch refuses; never adopt a caller PID or guardian identity. No-scale still calls `module.main()` unchanged.

Derive context from the existing source-verified loader, fixed private contract/provider-source paths and seal-verified original baseline; never accept caller-selected paths or derive originals from today's live roster. Check runId/feedSession/provider-context digest, target binding and exact baseline capture/boundary paths before injection. `sourceHashes` is the exact existing11-key collector-control verified subset; it is **not** the full lane bundle. Do not incorrectly assert `digest(sourceHashes)==bundleSha256`. The provider run-context ties the full bundle; context records both. Validate the injected values and compare supplied source bytes to the already verified set; invalid context refuses capture rather than silently disabling witnesses.

Write canonical private `progress-binding.json` once before polling. Its bytes are the exact validated context plus `captureDirectoryIdentity:{device,inode}` and `schemaVersion:1` (one schemaVersion field, not nested duplicate). Hold the directory fd and verify it still names the same nonsymlink mode0700 owned directory on later operations. Every witness contains `bindingSha256=SHA256(exact progress-binding.json bytes)`. This record contains identities/hashes, no credentials or target addresses. The expected baseline/deployment/roster remain independently recheckable through `baselineSha256`; copying an arbitrary caller context is not provenance.

## Exact written-frame references

Use exactly this `FrameRef` schema:

```text
{basename, device, inode, offset, length, sha256, at}
```

`basename` is `segment-NNNN.jsonl` with the existing ordered positive index; offset≥0; length in1..2MiB; device/inode are actual file identity; sha256 hashes the **actual UTF-8 line bytes including the final newline**; `at` is decoded from that line. No absolute path. Use strict integers excluding bool, finite numeric times, canonical SHA strings and duplicate-key-rejecting JSON.

For the opted-in capture, obtain the current tail length before `window.append(frame)`, allow that existing call to clean/filter and fsync, then open the exact segment with O_NOFOLLOW and read only `[offset, currentSize)`. Require private owned regular mode0600/nlink1 file, expected inode if pre-existing, length≤2MiB, one complete newline-terminated JSON object and no extra appended bytes; recheck file/directory identity and size after the bounded read. The collector is the sole writer. Hash/decode these actual bytes; never hash a reserialization of the original frame, because `CaptureWindow.append()` filters events. Preserve only the previous and latest references, including across segment rotation. This adds O(one frame) I/O, not an O(capture length) scan.

A later admission reader `read_progress(directory, expected_context)` uses held-directory-relative `pread` for each referenced range, with the same privacy/identity/length/newline/hash/at checks, and returns the validated record plus decoded previous/latest actual frames. A partial referenced line, changed segment inode, truncated range or mismatched hash refuses. It does not scan other segments or assume an arbitrary matching JSON object is the recorded frame. Read progress.json with a bounded stable-fd/identity check; its inode may legitimately change between reads due to atomic publication, so reject concurrent replacement during a read, not all replacements since an earlier read. The immutable capture binding/directory identity must remain exact. Initial zero-frame progress is valid diagnostic data but never finish admission evidence.

## Rolling progress schema and update order

`progress.json` has exactly:

```text
{schemaVersion:1, bindingSha256, generation, attempts, committedFrames,
 firstAt, lastAttemptAt, lastCommitMonotonic, maxGapSeconds,
 flags:{pollError,identityChanged,sqlFailure,skipped,invalidEvidence},
 previous:FrameRef|null, latest:FrameRef|null}
```

`generation` advances for every publication; attempts counts every scheduled capture attempt, including failed reads; committedFrames counts only durable recorded frames. firstAt is null before the first durable frame, then immutable. lastCommitMonotonic is null before that frame and is the capture worker's clock after frame fsync. `lastAttemptAt` records actual attempt time. Every flag is a sticky boolean (false→true only) for this same collector process; no field reset after clean later polls. `maxGapSeconds` is the maximum positive timestamp gap between successive durable frames; negative/non-increasing timestamps set invalidEvidence. Any gap>3s is retained in the maximum and prohibits later admission. A failed poll sets pollError even if a useful partial frame can still be appended. Do not advance committedFrames/latest/lastCommitMonotonic when no line became durable.

Compare each raw capture's exact name/uid/containerId/processId pair and Deployment UID/generation/desired against originals **before sanitization drops container/deployment fields**. Missing identity data sets invalidEvidence; changed identity sets identityChanged. During measurement require original clients not final and the expected ready state; during settling allow readiness to become false and exact original finals. Do not treat intended Ready=false after finish as identity drift. A new process cannot replace an old final.

Use existing `observed_sql_failure(raw_frame, firstAt)` semantics for sticky sqlFailure and require authenticated original client identity before calling it evidence of this run. Positive Skipped sets skipped separately, never SQL FAIL. Malformed counters/events needed for these bounded checks set invalidEvidence. This witness performs identity, poll/gap and adverse-outcome tracking only; do not duplicate transition/event-accounting/metrics acceptance from the full assessor. Later admission validates advancing clean SQL and exact metrics on the **actual last frame pair** using existing routines.

Update order: increment attempt/observe raw evidence → append and fsync frame if possible → validate actual appended bytes/update refs → atomically publish/fsync progress. On failed append, publish sticky error with unchanged tail if possible. Write a fresh private temp file, fsync it, atomic rename to progress.json and fsync parent; refuse symlinks/replacement directory/foreign owner. Use bounded reads of the exact canonical record. A write failure is an error, not permission to reset progress or mint a fresh capture. Until a valid progress record with two refs exists, admission fails closed. `progress.json` is provisional; it contains no PASS status.

## Fixed boundary records

Preserve existing `request.json`, exact-equality `ack.json`, and `receipt.json` schemas. New private records use O_EXCL/no-replace atomic publication plus fsync and never authorize a new request.

`boundary-start.json`, published after the first settling frame has been fsynced and **before** matching ACK:

```text
{schemaVersion:1, bindingSha256, requestSha256, requestId,
 originalRosterSha256, startedMonotonic, deadlineMonotonic,
 firstSettlingFrame:FrameRef}
```

The existing worker `settle_start=now` occurs before the first settling capture. Use that exact value; deadlineMonotonic is exactly startedMonotonic+5. Do not restart the clock when writing ACK or receiving exec responses. Both values are worker-host-only and bound to collectorIdentity.bootId. If first-frame fsync or start-record publication exhausts the deadline, no successful completion may follow. Original roster hash is canonical baseline roster; request hash is of the actual private original request bytes, not a caller copy.

`boundary-completed.json`, written only after existing exact boundary success conditions and actual fsynced final frames/finish receipt are available:

```text
{schemaVersion:1, bindingSha256, requestSha256, requestId,
 startSha256, receiptSha256, finalDurableMonotonic,
 finals:[{uid,processId,frame:FrameRef},{uid,processId,frame:FrameRef}],
 lastFrame:FrameRef}
```

Two finals are exactly the original pair, sorted by uid, with actual final=true, Connected=0, Scheduled=Attempts+Skipped and no new Skipped. Keep a per-original final-frame reference in memory when the collector first durably records its final; the two finals may appear in different frames. Require existing ACKNOWLEDGED exact receipt/request/client equality. Measure finalDurableMonotonic **after** the relevant final frame and exact receipt are durably readable; require it≤start.deadlineMonotonic. The later caller must still return within its own shorter budget. The record is proof of the final data's durability time, not a claim that disk writes/scheduler execution have a mathematically hard maximum.

Publish this small record before `SegmentedCapture.finish()` hashes the final segment and scans the complete archive. Do not wait for summary.json or guardian exit to create it. Partial/missing/late finals never create boundary-completed; records already written remain for assessment. Refusal or cleanup must not erase known sqlFailure. The complete archive and ordinary assessor remain the sole source of a traffic PASS; neither boundary witness means PASS.

## U5a acceptance fixtures

Cover null-context output compatibility; exact original/source/collector binding mismatch; malformed contexts and unknown fields; actual filtered line bytes versus original input; offset/hash/newline/inode drift; segment rollover pair; failed poll/partial append/sticky gap/SQL/Skipped/identity flags after later clean frames; wrong or missing original container/deployment; ACK only after first settling fsync/start seal; one-final-then-two-final order; exact5s boundary and late refusal; receipt mismatch; no completion on missing final; completion witness precedes full hashing/assessment; no O(n) tail reader. Test by fixture clocks/files/fake I/O, not cloud. Preserve ordinary/no-scale outputs and release guards.

