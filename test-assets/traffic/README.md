# Retained traffic evidence

The operator collector runs on the verified ACK worker using its authorized
Kubernetes configuration. It discovers the test-client Pod UIDs every second,
reads each process's counters and event sequence directly, and fsyncs allowlisted
JSONL outside the Pods. No database credential, SQL payload or auth token is
retained. Give each run a fresh private output path. Duration is bounded to 90
minutes. The collector only reads cloud and Kubernetes resources.

The integration operator supplies an atomic provider-inventory JSON file with
schema 2, one feed session, successful-publication sequence, `observedAt`,
`desired`, complete ESS `members`/`memberStates`, and NLB `backendStates`/
`healthyRegistered` IDs. Historical schema-1 captures retain their original
interpretation.
Refresh it from authoritative provider APIs every 15 seconds; age over 30 seconds
makes assessment inconclusive. Prometheus coverage requires exactly those members,
`up=1` scoped with `job="pgcat",env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"`
and pool queries scoped with `env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"`, plus complete active/idle/waiting triplets for every observed pool/user and
the expected test/poc_app identity, all with original raw source timestamps under
45 seconds. Missing Prometheus
makes the coverage inconclusive, while already fsynced traffic evidence remains.

The new client emits `traffic_final` after scheduling stops, all operation deadlines
settle and sessions close; a controlled finish holds final HTTP evidence for two minutes (ordinary SIGTERM retains its existing bounded shutdown behavior).
The collector retains that final sample before a Pod disappears. A missing final
sample is inconclusive. Final snapshots are cached by (Pod UID, container ID) so shutdown of an
already finalized HTTP endpoint cannot erase its retained terminal counters. A newly discovered process requires all startup events
and no history before the capture boundary. Do not infer old counts from new zeros.
Rolling restart before deploying this client revision cannot prove the old process
boundary; begin acceptance only after the new revision has stabilized.

PASS requires successful observed traffic, covered event/counter increments,
settled boundary work, an active client roster with scheduling progress throughout
the measurement phase, complete provider/metrics coverage, ordered polls no more
than three seconds apart and an explicit completion marker. Failure, timeout or
ambiguous SQL outcomes produce FAIL. Skips, cancellation, truncation that loses
needed events, unknown lifecycles or missing observations produce INCONCLUSIVE.
A separate settling phase extends the boundary by at most five seconds; it cannot
resume measurement or conceal stopped traffic during measurement. Completion is
required explicitly, including for direct assessment calls. The ring's historical truncation flag alone is not a gap if every required
sequence since the retained boundary is present.

Run fixtures with:
`python3 -m unittest discover -s test-assets/traffic -p 'test_*.py'`.
Run `worker_capture.py --output <private-new-path> --inventory <private-path>
--seconds 60` on the worker with both Python files in the same directory. Preserve
JSONL, completion and summary together. Assessing an interrupted file requires
`completed=False`; do not fabricate its completion marker. Integration stores
bounded artifacts in private encrypted OSS with expiry; local output is bounded
by this run's duration. Source code alone does not establish live qualification.

## Bounded authoritative provider feed

Run `provider_feed.py` on the Mac only after runtime access and ConfigMap update
are authorized. This tool reads Alibaba APIs through profile `infra-ops-poc` in
`ap-southeast-1`, scoped to ESS `asg-t4necb4dfmu6o3z2rn0g`, server group
`sgp-xs5s42tihtl6ja2a67`, and VPC `vpc-t4n4fi6r1a7bi6n93ftq3`. It uses the
project-root `working/tools/workbench/workbench` to publish to worker
`i-t4nj1bitmcz7cuoch82h`. Supply absolute paths, a fresh unique worker inventory
path in a private directory, and the already authorized worker kubeconfig path:

```sh
python3 test-assets/traffic/provider_feed.py \
  --workbench /absolute/project-root/working/tools/workbench/workbench \
  --worker-inventory /root/private-run/provider-inventory.json \
  --kubeconfig /root/private-run/authorized-kubeconfig \
  --interval 15 --max-seconds 5400
```

No credential is fetched or transferred by this tool. The worker must already
have Python 3, kubectl, and authorized Kubernetes access; the kubeconfig must be a
regular mode-0600 file. The default listener `lsn-7kuvcqkaz5o3z2w376@6432` comes
from historical configuration and is validated against live listener attributes
and health on every read. Supply `--listener-id` if the reviewed binding changes.
No saved configuration or historical listener value establishes current health.

ESS pages must have stable counts and page numbers with complete distinct IDs
bounded to the four-node test. The feed retains every documented ESS lifecycle
and Healthy/Unhealthy state, plus every NLB Adding/Available/Configuring/Removing
backend. NLB enumeration follows every cursor with stable counts and reads both
`GetListenerAttribute` and `GetListenerHealthStatus`. Unknown states, incomplete
pagination, or mismatched listener/server-group scope refuse publication.
`healthyRegistered` contains exactly current ESS IDs with an Available,
positive-weight backend and Normal listener health; Normal is inferred only from
absence in the complete non-normal list under enabled health checks.

ECS must supply one distinct private address in the exact VPC for every backend
and every non-pending member. A Pending/Pending:Wait member absent NLB may remain
addressless across fresh ticks only while it has never had an address, appeared
in NLB, or reached a non-pending lifecycle in this process. Its state explicitly
records `addressKnown=false`. No previously required address may silently vanish.
The default `--mode full-replacement` preserves historical schema-2 behavior:
the process remembers exact earlier ESS IDs and sequential desired decreases
4→3 and 3→2, each once. Earlier ESS IDs absent now must fit that departure count;
only those exact earlier IDs may remain NLB backends after leaving ESS. Approval
receipts remain a separate final acceptance gate.

The explicit `full-replacement-direct-v2` mode describes a single direct **2→4→2**
transition. Its first publication binds exactly two originals at desired two.
Desired four may converge through two and three physical members, but both exact
originals and every observed replacement must remain until the full four-member
cohort has been observed. The one desired-four to desired-two drop permits only
those two originals to depart, either together or through four/three physical
members while desired stays two. Both exact replacements must remain; desired
three, another expansion, new identities after the drop, and original reappearance
are rejected. Completion requires exactly the replacement pair as members,
backends and healthy registered IDs, plus both originals retired by the existing
complete-zero and later-backend-absence evidence. Fully covered nonzero old series
cannot complete this new mode; later nonzero samples or re-registration invalidate
the corresponding retirement proof. Historical modes retain their existing completion rules.

This mode is recognized by the CLI, producer and retained-evidence sanitizer for
source and fixture integration. The current CLI still requires the existing native
run context, whose admission accepts only `disabled-rehearsal`; selecting the direct
mode therefore refuses runtime startup before provider reads or publication.
Native observer integration and receipt admission require separate reviewed work.
The mode does not change any source age, receipt, deadline or metrics rule. Each
original retains its metrics obligation until complete fresh zero triplets across
its pool/users and a later fresh provider publication establishes backend absence.
A desired decrease or ESS departure alone never drops its targets or metrics
obligation. Learned scrape targets remain retained until authorized explicit
cleanup, including after an original's evidence obligation has been retired.

The worker file is atomically replaced in its own directory, fsynced and mode
0600. Schema 2 contains `schemaVersion`, `feedSession`, `sequence`,
`observedAt`, `desired`, sorted `members`, sorted `memberStates`, sorted
`backendStates`, and sorted `healthyRegistered`, plus optional `mode`. An absent
mode means `full-replacement` for historical evidence. A member row contains `id`,
`lifecycle`, `health`, and `addressKnown`; a backend row contains `id`, `status`,
`weight`, and `listenerHealth`. `feedSession` is one random non-secret UUID for
the process lifetime. `sequence` starts at one and advances only after successful
publication. A restart has a different session and cannot silently continue the
same assessed window. Private addresses exist only transiently for metrics target
reconciliation; they never enter worker inventory or retained JSONL.

The feed reads existing `raptor-test/pgcat-provider-inventory` targets at startup.
It retains all targets learned in successful publications throughout the process,
including nodes no longer in ESS/NLB, so later zero-client evidence remains
observable. A changed target list is patched with its current resourceVersion and
read back exactly in the same remote script before matching worker publication.
Unchanged exact lists skip the update; the single-operator gate and fresh metrics
assessment cover external drift. It never creates the ConfigMap or cloud resources.
Targets use existing PgCat port 9930 and DP00001/D00002 catalog labels. For a full
replacement, after the assessed window closes, explicitly reconcile to the exact fresh final ESS/NLB
fleet and verify targets during authorized cleanup; restarting this feed does not
repair or preserve the evidence continuity of the earlier window.

For the separately authorized disabled **2→3→2** rehearsal, add
`--mode disabled-rehearsal` at feed startup. The local provider requires the
adjacent `capture_window.py` module; worker packaging is unchanged. This mode
is retained in every inventory and capture segment, then revalidated during
private archive assessment. The same feed session must begin at desired two
with exactly the two originals, observe desired three and exactly one new ID,
and observe desired two before that new ID departs. Originals cannot depart,
IDs cannot change or reappear after departure, and mode/session/sequence cannot
change. Desired four, a skipped expansion, or a second expansion is rejected.
The final inventory must contain exactly the original healthy two backends and
members. A no-scale capture cannot pass as a rehearsal; use the default mode
for the separate no-scale plumbing check. All metric, SQL, freshness, sequence
and completion requirements remain unchanged.

Keep this same feed and capture live through the later new-backend absence
publication, explicit resourceVersion-bound reconciliation/readback to the two
original targets, and a subsequent feed publication/readback plus complete fresh
two-node metrics. The feed retains its learned target history; do not restart it
or alter that history to reconcile. An unchanged target list skips rewriting the
ConfigMap, so the explicit final cleanup remains in place on subsequent ticks.
Stop mutations on uncertain readback, then preserve evidence for reviewed
recovery. Finalize the capture only after that in-window closeout proof.

The loop starts a read at most every 15 seconds and lasts at most 5,400 seconds.
Each refresh carries one absolute deadline capped by the interval and remaining
runtime. Provider and Workbench subprocess timeouts use the remaining budget;
before each remote operation the tool probes the exact worker's monotonic clock
and Linux boot identity over Workbench. A probe round trip over two seconds, an
invalid sample, or insufficient remaining budget refuses the operation. The
worker expiry subtracts the full measured round trip from the budget at send,
plus a 100 ms margin; this uses the earliest possible sample time. The remote
process checks the boot identity and monotonic expiry, remote kubectl timeouts
use that expiry, and file replacement checks it again after fsync. A reboot or
delayed command therefore refuses publication. Wall-clock offset between Mac
and worker does not extend this deadline; no wall-clock synchronization
assumption is used.
A refresh/publication that misses its interval,
provider refusal, or Workbench failure stops the process with sanitized status;
it never renews freshness using cached inventory. The previous file remains
available and becomes stale for assessment after 30 seconds. A target update
that succeeds before a failed worker write may remain applied: stop and reconcile
before restarting. Remove run files during the authorized run cleanup.

Offline fixtures:
`python3 -m unittest discover -s test-assets/traffic -p 'test_provider_feed.py' -v`.

### Continuous segmented worker capture

`worker_capture.py --output /private/fresh-capture-directory --inventory inventory.json --seconds 5400`
creates a new mode 0700 directory. Duration is 5–5400 seconds; one collector
and one one-second poll clock continue through approval waits, Pod restarts,
and rotations. `query_metrics` and `capture_frame` keep their existing interfaces.
Segments rotate at 1800 seconds, retain the same `CaptureWindow` event sequence
state, and are never reopened after rotation. The sole end settling phase is
bounded to five seconds. Segment JSONL, `completion.json`, and `summary.json`
are mode 0600. Unavailable polls remain in the completion manifest.

Reassess retained evidence with `segmented_capture.assess_manifest(Path(directory))`.
The manifest binds ordered relative filenames, SHA-256 hashes, timestamps,
frame counts, unavailable poll count, and each observed process's final marker.
All segments form one assessment stream. Missing, altered, overlapping, reordered,
or unlisted segments and missing client final markers prevent PASS; a failed SQL
operation retains FAIL precedence. A later clean segment cannot repair an earlier
coverage loss. Running clients must provide explicit settled final snapshots for
completion; a finite collector timeout alone does not establish client finality.

For a disabled-mode timing diagnosis, add `--timing-output /private/new-timing.jsonl`
with a fresh path outside the capture directory. This optional mode-0600 log
records each attempt's capture and evidence-append duration, including reads
that fail before a frame is retained. It records no exception text, endpoint,
credential, or client identity. Keep it private with the capture artifacts.
The assessor does not read this log; it does not change the three-second gap
rule, missing-poll count, or traffic verdict.

### Exact client finalization boundary

After separate action-time authorization for client finish, start the collector
with a fresh shared private directory:

```sh
python3 worker_capture.py --output /private/new-capture --inventory inventory.json --seconds 5400 --boundary-dir /private/new-boundary
python3 finish_clients.py --boundary-dir /private/new-boundary --expected-replicas 2
```

Run the second command in a separate session after fleet convergence. This helper
performs a runtime mutation: exactly two concurrent bounded `kubectl exec` calls
invoke `/client finish --expected-pod-uid UID --expected-process-id PID`. It reads
current Pod names, UIDs, container IDs and evidence process IDs, writes one unique
request, waits for the collector acknowledgement, and rechecks the exact roster
before dispatch. No shell or TCP finish endpoint is used. The directory is mode
0700; atomic request, acknowledgement and sanitized receipt files are mode 0600
and fsynced. A directory/request cannot be reused for a second finish action.

The collector appends and fsyncs the first settling frame before acknowledgement.
It preserves one SegmentedCapture and one poll clock, including all earlier
process finals across segment rotations. It waits for every observed process
final and the helper receipt, for at most five seconds from the boundary. One
final client cannot close the window while the other is active. A container
restart in the same Pod is read anew. Changed or incomplete identities, malformed
boundaries, missing/uncertain receipts, late polls or missing finals prevent PASS;
observed SQL failure retains FAIL precedence. Retain all boundary files with the
capture. The two-minute client hold does not extend this assessment deadline.

An unknown exec result is never retried. The helper reads current identities and
reports only whether the roster is unchanged; this does not prove finality or
repair the result. Preserve evidence, reconcile the exact `/evidence` snapshots
and Pod/container identities, and stop replacement mutations. Raw kubectl output,
DSNs, kubeconfig and Secret values are never persisted by this helper. A successful
receipt alone cannot establish traffic acceptance. These commands document a
path requiring runtime authorization; implementation does not authorize a run.
