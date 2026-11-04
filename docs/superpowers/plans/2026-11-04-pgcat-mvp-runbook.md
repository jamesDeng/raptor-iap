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
day. Keep the 21:30 Shanghai start cutoff and 22:30 reconciliation/stop cutoff,
and the existing resource shutdown. Recording must leave time for assessment and
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
utility is Python3.6-compatible and uses only the standard library and kubectl.
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
