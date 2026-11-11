# Active PgCat MVP: ordinary app logs on the existing worker

Owner-approved direction, 2026-11-04. This is the active operator entry and
supersedes the collector-dependent qualification, enablement, closure and finish
requirements in [the retired live-test runbook](2026-10-16-pgcat-two-node-live-test-runbook.md)
and [legacy traffic tooling](../../../test-assets/traffic/README.md).
Those implementations remain for historical reproduction. Do not run the native
collector controller, a synthetic traffic-frame proof, a timing assessor or a
custom collector qualification as an MVP prerequisite. No new service, storage,
IAM or runtime release is needed for this acceptance change. Runtime
`acceptance=pending` stays unchanged; ordinary logs support an operator report.
The [approved design](../specs/2026-11-04-pgcat-mvp-logs-design.md) controls scope.

For the current request-grants flow, use the [verified Raptor Agent replacement
procedure and acceptance record](#verified-raptor-agent-replacement-on-2026-11-11).
The original MVP procedure below retains its November 4 release and acceptance
settings.

## Source, idle state and preflight

Use the current reviewed source, immutable `skills-v0.5.0` resource-v1 release,
normal Raptor / Infra MCP / Infra API / Prometheus route and deployed
`gpt-6-luna` support. Record Git/skill/definition hashes, pinned image digests,
actual imageIDs, bound Deployment UID, guard/helper hashes and live versions.
Local tests and hashes alone do not establish deployed source. Review current
changes and run the applicable offline checks before staging the exact source.
Use the Codex in-app browser's signed-in Raptor session for web work and local
Workbench CLI for ACK/worker operations; read the project's ACK Workbench skill.

Check the day's cost/balance/forecast once, including the four-node peak and
cleanup within the current CNY3,000 net POC ceiling (owner confirmed November2); reuse that observation that
day. There is no fixed start cutoff (owner confirmed November 6). Start only
when the run and its cleanup fit before the 22:30 Shanghai reconciliation and
stop cutoff. Keep the existing resource shutdown. Recording must leave time for assessment and
cleanup. A 7,200-second utility ceiling is not permission to exceed those cutoffs
or any existing operation/approval timeout.

Freshly verify `replacementEnabled=false`, Gateway idle and no unresolved
request, approval, continuation, lease, outbox or uncertain earlier operation.
Quiesce all request/approval/continuation producers around guarded flag changes
and Gateway rollouts; an idle snapshot is not an admission lock. Rediscover
current exact scope: `rdev.ali`, `DP00001`, `D00002`, `A00003`, Singapore,
ESS `asg-t4necb4dfmu6o3z2rn0g`, NLB group `sgp-xs5s42tihtl6ja2a67` and ACK
`c92787e953503492ea141a744c81498f1`. Require desired2/min2/max4, two protected
healthy originals with matching healthy backend registration, application2Ready
and fresh complete scoped provider/metrics observations and SQL probes. Historical
node IDs, approvals and observations are not current preflight.

Use only existing worker `i-t4nj1bitmcz7cuoch82h` for recording, never either
PgCat retirement candidate. Verify Python3, kubectl, reviewed source and at least
512MiB free disk. Reuse valid authorized access, otherwise use the standing
72-hour temporary kubeconfig authorization for this exact cluster/worker.
Keep access separate from logs; private access commands, values and actual
credential paths stay out of Git. Plan removal of both exact owned temporary
kubeconfig copies after completion, failure or stop. No cloud provisioning.

## Record logs before any change

The small [app_log_retention.py](../../../test-assets/traffic/app_log_retention.py)
utility requires Linux with `os.waitid(WNOWAIT)`, is Python3.6-compatible and uses
only the standard library and kubectl. Exit observation keeps each owned group
leader unreaped until descendant cleanup, then releases ownership permanently;
historical stream records are never later signaled. Other platforms support help
but refuse recording explicitly.
It binds Deployment `raptor-test/rdev-test-client-client`, UID
`17b7b33c-fd64-4d07-863e-f65b6563e504`, container `client` from the app chart.
It follows the Deployment→ReplicaSet→Pod controller-UID chain, including surge
Pods and restarted containers. Labels alone never admit a Pod.

Prepare a dedicated private retention parent and a unique, nonexistent run
directory on this worker. Set the following operator variables locally to those
reviewed destinations; they are placeholders, not committed private paths.
`APP_LOG_LAUNCH_LOG` must be a new private launcher-output file outside the run
directory. Supply Kubernetes access through the existing environment separately.
Launch detached before the disabled restart or any Gateway/config change:

```sh
umask 077
set -C
nohup python3 app_log_retention.py \
  --namespace raptor-test --deployment rdev-test-client-client \
  --deployment-uid 17b7b33c-fd64-4d07-863e-f65b6563e504 \
  --output-dir "$APP_LOG_RUN_DIR" --seconds 7200 \
  >"$APP_LOG_LAUNCH_LOG" 2>&1 < /dev/null &
APP_LOG_PID=$!
set +C
```

The utility creates the run directory0700, logs/manifest0600 and refuses an
existing directory or symlink ancestor. Use a canonical real directory path
(for example, resolve a system temporary-directory symlink before choosing the
new child). No existing evidence is overwritten. Default cumulative log cap is
256MiB; `--max-bytes` may lower it. Metadata is separately bounded, with a 1MiB
emergency status reserve and at most1,024 streams. Metadata API and subprocess
deadlines are five seconds; intentional `kubectl logs --follow --timestamps`
streams use no request deadline. Current old containers start at the recording
timestamp; newly discovered containers request all available start history.

Read `manifest.json`: status must become `READY`, with both current initial
PodUID/containerID identities recorded and nonzero bytes for each, before any
change. Open both bound log files and verify actual app JSON output, identity,
timestamps and advancing operations. Record the old Pod UID/containerID roster.
`READY` establishes initial log receipt only. This utility does not parse SQL,
sample metrics, score traffic or invoke infrastructure operations.

## Disabled restart and retention check

Keep Gateway disabled and idle. Recheck the exact Deployment UID and reviewed
image before one ordinary rolling restart of that bound application only.
Record the restart receipt, template/generation and ReplicaSet transition.
Observe the same detached recorder discovering new owned PodUID/containerID
streams, including surge, without replacing or splicing the capture directory.

Verify rollout convergence to2Ready and independently confirm both recorded
old Pod UIDs are actually deleted. Open their retained worker files after that
deletion; inspect both old and successor app output and manifest exits/problems.
Record any missing finals, SQL failure/timeout/ambiguous result, skipped work,
restart, unavailable history or capture gap. Stop recording as below and retain
the manifest/files plus sanitized observations and hashes. This proves only the
observed disabled restart/log retention, never full replacement acceptance.
A failure/gap is preserved for review; do not erase it with a later clean window.

## Fresh qualification and one normal full request

After the disabled check, obtain fresh resource/provider/metrics/source facts and
repeat the normal read-only Raptor qualification with its human approval
pause/resume. Keep request identity, immutable definition/skill binding,
authorization causality and current source-freshness checks. Routine standing
authorization persists; exact business approvals remain action-bound.

Start a fresh detached app-log run and verify both current logs before enabling.
With producers quiesced, current idle state verified and no unsettled operation,
use the existing reviewed Go `gateway-live-gate` Secret/version/idle flow:

```sh
gateway-live-gate --namespace raptor-system --secret gateway-live-config --expected false --set true
gateway-live-gate rollout --expected true --resource-version RECEIPT_VERSION --changed-after RECEIPT_TIME
gateway-live-gate verify --expected true --resource-version RECEIPT_VERSION --changed-after RECEIPT_TIME
```

Use exact sanitized version/time from the set receipt. An unknown response,
conflict or verification mismatch requires reconciliation; do not replay a
possibly committed mutation. Resume only authorized producers and submit one
normal signed-in Raptor `db-proxy.replace-nodes` request in replacement mode,
model `gpt-6-luna`, immutable `skills-v0.5.0` resource-v1, exact resolved scope.
Record request/definition/skill IDs and preserve them across approval/resume.

Preserve the full2→4→2 sequence and existing business safeguards: qualify and
protect the two healthy replacements; withdraw both bound originals from new
connections before restarting the bound application; require fresh complete
causal active+idle+waiting observations across BOTH originals, every pool/user,
with combined total≤1; retain the exact healthy replacement pair. Never treat
one client per original as admissible. Desired must remain≥minimum2 and max4.
One exact desired-capacity reduction approval binds this request/action/scope and
the4→2 target. Recheck capacity, lineage, metrics and approval validity after
pauses immediately before dispatch. Minimum changes remain outside this MVP,
require explicit approval and belong to Terraform/GitOps. Keep all non-collector
timeouts, uncertainty, no-replay and source/causality safeguards.

## Reconcile, assess and clean up

Report runtime convergence separately from app traffic impact. Fresh provider
and metrics facts must establish desired2, the exact retained protected healthy
new pair, correct NLB registration/old absence, and application2Ready. The
unchanged runtime can report `acceptance=pending`; the operator separately
assesses retained ordinary app outcomes, old/new identities, capture coverage and
exact request/approval receipts. File existence, a stopped recorder, a completed
rollout or fleet convergence alone cannot establish traffic success. Report SQL
failures, timeouts, ambiguous outcomes and gaps; accepted one-connection exposure
is not zero-loss evidence. There is no new mandatory finish command, synthetic
frame contract or three/five-second collector completion gate.

Quiesce producers, reconcile uncertain operations and restore disabled through
the same Go guard (`--expected true --set false`, then rollout and verify using
that restore receipt's exact version/time). Verify effectivefalse/idle on the
Ready Gateway, final fleet/app state and no unresolved work. Do not reverse an
uncertain mutation automatically.

Stop the recorder explicitly by creating a regular0600 `STOP` file in its exact
run directory or sending SIGTERM to the recorded recorder PID after confirming
its identity. Wait for final `endedAt`, every stream's exit code/time and actual
process absence; the utility drains available stdout, terminates/reaps its owned
children and flushes logs. A retiring container gets a short natural-EOF grace;
if its stream must be interrupted, `retired_stream_interrupted` preserves the
potential final-output gap. This is process cleanup, not a traffic completion
gate. `COMPLETED` means an explicit local recording stop
without detected utility problems, never traffic PASS. `UNRELIABLE` preserves
missing initial logs or container restarts; `FAILED` preserves unexpected stream
exit, metadata/write/disk errors, forced kill or duration/byte bounds. Problems
are sticky. Raw command stderr and credentials are never included in status.
Rotation, a Pod created/deleted between discoveries, and name-based log-API races
can still lose history; the ordinary streams are not a perfect-coverage proof.

Remove and verify absence of both exact temporary access copies and any owned
temporary control/access artifacts on success, failure or stop. Keep logs and
the sanitized operator report in the dedicated retention directory for7days at
low cost. Cleanup may remove only reviewed expired runs under that dedicated
parent, after assessment; preserve the current run and unrelated evidence.
The utility itself deletes no prior run or unrelated file. Record actual cleanup
and remaining uncertainties before the cutoff; do not infer them from an exit.

## Verified Raptor Agent replacement on 2026-11-11

Raptor request `8a0cb7f0` completed a full **2→4→2** in `rdev.ali` on PgCat
`DP00001` with test application `A00003`
(`raptor-test/rdev-test-client-client`). It ran GPT-6.1 Sol with
`skills-v0.5.16`. The request-scoped grant covered expansion, node protection,
drain, one rolling restart and retirement. The operator verified read-only
qualification with replacement disabled, confirmed that Progress showed agent
updates and tool events, and received a reply to a troubleshooting message.
The operator enabled the test gate and resumed the original request. Raptor
Agent executed all five writes through the authorized tools, and the operator
restored the gate to disabled after acceptance.
[Replacement request](https://raptor.rdev.raptor-iap.top/#request=8a0cb7f0-4ea3-4940-b9dc-6eec1292f194) ·
[Read-only qualification](https://raptor.rdev.raptor-iap.top/#request=54a36f25-b07a-4999-917d-aaec289cb8f5)

### Replacement and SQL acceptance

1. Confirm the two healthy, protected originals, both at NLB weight 100.
   Record the bound application's ready generation and baseline SQL. Hold a
   live, identified TCP connection to each original and execute SQL on each.
   Expand once to four. Wait for both replacements to be healthy, registered,
   queryable and visible in metrics. Protect the exact replacement pair.
2. Drain the exact originals by setting their Alibaba Cloud NLB backend
   weights to zero. Keep them registered and running. After the recorded
   post-drain wait, execute SQL on the same two held TCP sockets. Separately,
   open new connections and verify that they succeed and reach only the
   replacement pair. Save both results to the request checkpoint before
   releasing the held probes. Stop if any held SQL fails, any fresh connection
   reaches an original, or the evidence is incomplete.
3. Roll the bound application once and wait for the new generation to be
   observed with all replicas updated, ready and available. Confirm real
   application SQL on **each** replacement. Wait until complete PgCat metrics
   show **zero active, idle and waiting client connections on each original**.
   A missing series is incomplete evidence. Weight zero and elapsed drain
   time only describe routing and timing.
4. Scale from four to two once, targeting only the two original instance IDs.
   Confirm the matching ESS retirement activity succeeded and removed those
   exact IDs. Verify the retained pair is healthy, protected and routed at
   weight 100, with the application still ready. Sample final application SQL
   on both retained nodes and confirm Raptor shows Completed, a verified
   checkpoint and confirmed cleanup.

### Waiting, rereading and continuing after HTTP 503

The initial attempt `1cb4a467-03a2-43ae-b965-414c0f6c569a` returned HTTP 503
from `cloud_read` with `kind=fleet` before expansion. The upstream error
category was unavailable. Gateway now waits one second and retries this read
once on runtime unavailability within the existing read deadline, rechecking
authorization, lease and cancellation before the retry. A second failure
returns the error. This retry applies to the fleet observation.
[Fleet retry and recovery](https://github.com/jamesDeng/raptor-iap/pull/447) ·
[Gateway deployment](https://github.com/jamesDeng/raptor-iap/pull/448)

- **Before any write.** If the attempt ends in `ToolFailed`, use **Continue**
  on the same request once Gateway supports this recovery. Require the verified
  checkpoint and confirmed sandbox, key and access cleanup, successful grant
  lookup, the recorded 503 on the cloud observation, and no write starts or
  results in the retained history or durable action journal. Raptor revalidates
  the saved approval and scope. Refresh the two-node baseline and verify the
  same healthy, protected originals. The resumed attempt restores the logical
  hold and opens fresh physical sockets before the first write. Use those
  sockets for the later same-connection drain check.
- **After a write.** Match the original request, node IDs and action receipts
  to the live state. A pending provider transition or unavailable observation
  calls for `resource_wait` and a reread of the scoped selector. If a write's
  outcome is unknown, reconcile that action from the journal and live provider
  state before taking another action. Keep verified drain evidence and resume
  from the first unfinished step. Once scale-in is recorded, resume with
  read-only retirement and final SQL checks using its saved receipt. If the
  checkpoint, identities or receipts disagree, stop and investigate. Do not
  replay expansion, drain, restart or scale-in.
  [Post-retirement wait](https://github.com/jamesDeng/raptor-iap/pull/442) ·
  [Post-retirement 503 recovery](https://github.com/jamesDeng/raptor-iap/pull/444)

### Evidence from request 8a0cb7f0

All times are on 2026-11-11 in Shanghai (UTC+8). The original nodes were
`i-t4n7kle6pj89zjalm3gc` and `i-t4n7kle6pj89zjalm3gd`. The replacements were
`i-t4nbfsik5vzvkapm239x` and `i-t4nbfsik5vzvkapm239y`, shown as `239x` and
`239y` below.

| Time | Evidence |
| --- | --- |
| 18:26:27 | Fleet read returned 503. The attempt checkpoint verified, cleanup was confirmed and the request's write journal was empty. The same request and grant were reused. |
| Before expansion | Both originals were healthy and protected at NLB weight 100. Application generation 31 was 2/2 ready. Baseline SQL recorded 540 successes in 90 seconds with zero errors. |
| 19:10:33–19:19:21 | Logical hold recovered with the original node identities. One expansion was submitted at 19:11:10. Both replacements became healthy and registered. The protection action was submitted at 19:19:21. |
| 19:19:46–19:21:43 | The originals stayed registered and their NLB weights went from 100 to 0. Both held TCP sockets executed SQL after drain. All 24 successful new connections reached the replacements, 10 on `239x` and 14 on `239y`. The journal recorded verified drain evidence before restart. |
| 19:21:54–19:25:21 | One rolling restart advanced the Deployment from generation 31 to 32, with 2/2 updated, ready and available. At 19:25:12 Prometheus showed zero active, idle and waiting clients on each original. Pre-retirement traffic evidence recorded 314 successful queries and transactions on `239x` and 313 on `239y`, with zero errors. |
| 19:25:48–19:29:22 | One scale-in targeted the original pair. ESS activity `asa-t4ncpkz85h0u6ri1c8zd` succeeded and listed exactly those two retired instances. Both retained nodes were healthy, protected and at NLB weight 100. Each recorded 324 successful application queries and 324 transactions in the final sample, with zero errors. |
| 19:29:26 | Raptor reported **Completed**, checkpoint verified and cleanup confirmed. The durable journal recorded exactly five write actions, one per approved operation. |

The independent Prometheus observer recorded 36 valid 90-second SQL windows
with zero failures, timeouts or ambiguous outcomes. Some observer polls did
not produce usable samples. The 36-window count describes valid samples.
[Raptor Progress and Result](https://raptor.rdev.raptor-iap.top/#request=8a0cb7f0-4ea3-4940-b9dc-6eec1292f194)
