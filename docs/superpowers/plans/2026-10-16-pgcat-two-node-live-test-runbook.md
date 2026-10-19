# PgCat two-node live-test operator runbook

Status: sustained disabled-mode finish/restoration proof **PASS** on 2026-10-18:
62 measurement frames, 384 successful operations, zero missed polls, exact
two-client finals and 2/2 restoration. The first detached attempt was
INCONCLUSIVE after its cap elapsed before finish; both attempts were retained.
The provider feed exited cleanly, the evidence archive readback hash matched,
and both copies of the temporary kubeconfig were removed. **Full execution remains BLOCKED**
pending fresh preflight, explicit direction to change the currently false
replacement flag, and its separate action-time gates. Disabled-mode PASS does
not establish replacement acceptance.
This document authorizes no credential transfer, Secret change, rollout or replacement.
Use the approved [design](../specs/2026-10-16-pgcat-two-node-live-test-design.md),
existing `agent-skills/pgcat-replacement/SKILL.md` two-node strategy, and
`test-assets/traffic/README.md` interfaces. Historical receipts are not current preflight.

## Hard gates and action-time authorization

Scope: `rdev.ali`, `DP00001`, `D00002`, `A00003`, Singapore `ap-southeast-1`,
ESS `asg-t4necb4dfmu6o3z2rn0g`, NLB group `sgp-xs5s42tihtl6ja2a67`,
ACK `c92787e953503492ea141a744c81498f1`, worker `i-t4nj1bitmcz7cuoch82h`.
Rediscover exact scope and Deployment UID; mismatches require review, not editing a guard constant.
Expected originals at design time: `i-t4n5kisq7u820wbx8nri`, `i-t4n5kisq7u820wbx8nrj`.
Verify both actual baseline originals before creating the immutable request.

Before any live action, present fresh cost/balance and forecast including four-node
peak, runtime and retention within the owner-approved ceiling; record observation
time and source. The original ceiling was RMB 1,000. On 2026-10-18 the owner
raised the whole-POC ceiling to CNY 1,200 net pretax for this one test after
the billed October net reached CNY 959.08. This amendment does not remove the
fresh forecast, balance, delayed-billing, or stop gates.
Do not start after 21:30 Asia/Shanghai; stop new mutations by 22:45; reserve
reconciliation/credential deletion before daily 23:59 stop. Defer if the remaining
budget cannot cover the entire capture, approvals and cleanup. Capture maximum is
5,400 seconds, each segment maximum 1,800 seconds, provider refresh 15 seconds.

Verify signed-in Raptor creator/admin and supported replacement model
`gpt-6-luna` on the deployed Raptor and Gateway images; qualification model
support or a local source change is not replacement support.
Keep `skills-v0.4.0` immutable. Record resolved tag commit, selected skill SHA,
request definition SHA, source Git SHA, collector/provider source hashes,
worker artifact hashes and guard binary hash; record registry image digests,
live Pod imageIDs, rollout revisions and bound application UID. Build/deploy
through reviewed PR, image pin and Argo workflow. Source hashes do not prove live binaries.
Run image acceptance on the actual pinned image before declaring packaging verified.

Fresh preflight requires client 2/2 and metrics 1/1 ready; desired two/max four;
exactly two protected healthy ESS members, matching healthy NLB registration;
complete fresh `test`/`poc_app` metrics and read-only SQL probes; no unresolved
attempt, lease, checkpoint, approval, outbox work or sandbox/key/access cleanup.
Quiesce **all request, approval and continuation producers** before each flag set
and rollout and keep them quiesced until verification. `gateway --idle-check`
is a database snapshot, not an admission lock. If producers cannot be quiesced,
STOP. Recheck idle immediately before changes through the guard.

Present exact artifact hashes, targets, time/duration, credential paths, retention
prefix/expiry and deletion commands for separate authorization of (a) temporary
kubeconfig Workbench upload, (b) provider ConfigMap reconciliation and disabled
capture, (c) guarded enable/rollout and one replacement, (d) controlled client
finalization/restoration, and (e) settled disable/rollout and cleanup. Each requires
current action-time authorization; approval of this document alone is insufficient.
New IAM or cloud resources return to the component Terraform PR with sanitized
plan comment and reviewed apply. Do not provision via CLI or expand IAM here.

## Worker preparation and disabled 60-second measurement proof

Read project-root `working/skills/ack-workbench-cli/SKILL.md` before Workbench use.
Use only profile `infra-ops-poc`, region `ap-southeast-1`, exact worker above on
every call. Verify worker hostname, Linux architecture, Python 3, kubectl, clock
synchronization, disk budget, private API access and configured profile identity
without displaying credential helper output. Shell access does not prove Kubernetes access.

Only after specific upload authorization, obtain a temporary **private-network**
kubeconfig (default 1,440 minutes; API range 15–4,320), capture without output to
an exclusive mode-0600 local file, verify expiry privately, and upload through
Workbench to a unique mode-0600 worker file in a mode-0700 run directory.
Verify ACK identity and bounded Kubernetes reads. No stale kubeconfig, public API,
TLS bypass, alternate transfer or Secret snapshot is an acceptable substitute.
Register cleanup before acquisition/upload; if upload outcome is unknown, attempt
remote removal of that exact planned path and verify absence. No credentials in
arguments, transcripts, Git, evidence or receipts; `KUBECONFIG` contains a path only.

Transfer only reviewed collector modules (`worker_capture.py`, `capture_window.py`,
`segmented_capture.py`, `finish_clients.py`) and the hashed guard binary matching worker architecture.
Record worker SHA-256 readback. Use fresh private output paths; never reuse a capture directory.
Run the collector detached from the Workbench CLI `exec` that launches it.
An attached collector holds that Workbench session open and can block the
feed's next Workbench call past its 15-second deadline. The 2026-10-18
disabled capture saw a feed stop during an attached collector; a harmless
overlap probe made a concurrent `exec true` time out after 10 seconds, while
a correctly detached eight-second probe launched in 0.3 seconds and wrote
a mode-0600 exit status. This supports a Workbench contention diagnosis;
the original sanitized feed error does not identify its exact failing call.
Do not overlap Workbench CLI sessions intentionally. Launch, status reads,
finish and cleanup should be short, sequential calls; verify feed publication
and provider age after each. Any missed feed deadline or freshness gap makes
the capture INCONCLUSIVE. Never lengthen the feed deadline or publish cached
inventory to hide contention.

For either capture, use a fresh mode-0700 worker run directory and a unique
mode-0700 boundary directory. First verify `collector.exit` and
`collector.exit.tmp` do not exist. In a **short** Workbench `exec`, run the
following from the worker; replace every `PRIVATE_RUN` and `CAPTURE_ARGS`
with the reviewed exact paths and arguments shown below. Keep the semicolon
before `nohup`: joining `cd` and `nohup` with `&& ... &` can background the
whole command list and leave Workbench attached to its output.

```sh
cd /root/PRIVATE_RUN || exit 1
umask 077; export KUBECONFIG=/root/PRIVATE_RUN/kubeconfig
nohup sh -c 'python3 worker_capture.py CAPTURE_ARGS > collector.log 2>&1; code=$?; printf "%s\n" "$code" > collector.exit.tmp; mv collector.exit.tmp collector.exit' </dev/null >/dev/null 2>&1 &
printf '%s\n' "$!" > collector.pid
```

Treat `collector.pid` as a diagnostic only; never use a reused PID as proof of
process identity. Confirm the Workbench launch returns promptly and a separate
short status call finds the collector active or its final status. At closure,
require `collector.exit` mode 0600 with code 0, `completion.json`, ordered
manifest, hashes and assessed summary. Missing, malformed or nonzero status,
an early exit, or a Workbench timeout is INCONCLUSIVE pending inspection;
retain private logs and evidence. Do not launch a second collector over an
uncertain first one. Use the reviewed one-use worker control for finish while
the feed is active; check the feed's latest successful publication afterward.
Start the provider feed on the Mac from this repository (placeholders must be
resolved and authorized, not pasted literally):

```sh
python3 test-assets/traffic/provider_feed.py \
  --workbench /absolute/project-root/working/tools/workbench/workbench \
  --worker-inventory /root/PRIVATE_RUN/provider-inventory.json \
  --kubeconfig /root/PRIVATE_RUN/kubeconfig --interval 15 --max-seconds 5400
```

This reads authoritative ESS/ECS/NLB scope and listener health, atomically publishes
inventory and reconciles only the existing `raptor-test/pgcat-provider-inventory`
ConfigMap targets. It does not acquire credentials or create cloud resources.
The read-only ESS/ECS/NLB calls use Alibaba CLI `--retry-count 2`; any CLI-internal
retries must complete within the same absolute 15-second refresh deadline.
Unrecovered read, timeout or publication failure stops the feed; do not refresh
freshness from cached inventory. Workbench and ConfigMap writes are not retried.
Age >30 seconds, wrong roster or target discrepancy blocks acceptance. If target
update succeeded but inventory publication failed, reconcile before restarting.

With replacement flag still false, use the detached launch above on the
authorized worker with private `KUBECONFIG` and reviewed modules colocated.
For `CAPTURE_ARGS`, use:

```sh
--output /root/PRIVATE_RUN/disabled-60s \
  --inventory /root/PRIVATE_RUN/provider-inventory.json --seconds 75 \
  --boundary-dir /root/PRIVATE_RUN/disabled-boundary
```

The 75-second cap allows at least 60 seconds of measurement before the one-time
finish request and its bounded settling. Start the finish helper after at least
60 seconds of collector runtime, with enough time left before the cap to record
the request, both finals and receipt. Confirm at least 60 ordered one-second
measurement frames from retained evidence; elapsed wall time alone does not
prove coverage. If the finish cannot begin with that headroom, let the run end
INCONCLUSIVE and start a fresh authorized capture instead of rushing the gate.

Verify direct client Pod UID/container/process evidence access and private
Prometheus access, one-second continuity, exact provider roster, `up=1` and
complete pool/user active/idle/waiting triplets with DP00001/D00002/rdev.ali
labels and raw timestamps under 45 seconds. Discovery reads selector-matched
Running/Pending Pods with an IP directly, including unready Pods; it does not
filter on Ready or rely on headless Service DNS after finish.

In a separate authorized worker session, before the 75-second collector cap,
use the same private boundary directory and kubeconfig:

```sh
python3 finish_clients.py --boundary-dir /root/PRIVATE_RUN/disabled-boundary --expected-replicas 2
```

Keep `replacementEnabled=false`. The first 2026-10-18 controlled-finish
capture closed after 12 assessed seconds. A later detached capture retained
62 measurement frames and passed the exact finalization, feed, traffic and
restoration checks. Repeat this proof if the client image, provider feed,
runtime scope or other relevant implementation changes before a full run.
A disabled-path traffic PASS establishes only that observed window; it never
establishes replacement acceptance.

## Reviewed client boundary, finish and restoration

Before either disabled proof or full-run finish, verify the published immutable
image digest and live image IDs, `TEST_POD_UID` downward API, memory volume and
UID/GID 65532 against the reviewed GitOps manifest. Require exactly two stable
Running, initially Ready client Pods, distinct Pod names, UIDs, container IDs and
process IDs. The helper binds that exact roster from direct `/evidence`; retain
prior process finals from any mid-run restart in the same capture stream.

Start a full collector before any enabled replacement, using the detached
launch and closure checks above with fresh private output and boundary
directories. For `CAPTURE_ARGS`, use:

```sh
--output /root/PRIVATE_RUN/full-capture \
  --inventory /root/PRIVATE_RUN/provider-inventory.json --seconds 5400 \
  --boundary-dir /root/PRIVATE_RUN/full-boundary
```

After fleet convergence and separate action-time authorization for client finish,
run in a second session:

```sh
python3 finish_clients.py --boundary-dir /root/PRIVATE_RUN/full-boundary --expected-replicas 2
```

The helper writes one unique `request.json`; the collector fsyncs the first
settling frame before writing matching `ack.json`. The helper rechecks the exact
roster, then concurrently invokes exactly two bounded commands of this form:
`kubectl -n raptor-test exec EXACT_POD -- /client finish --expected-pod-uid EXACT_UID --expected-process-id EXACT_PROCESS_ID`.
The distroless binary uses its private mode-0600 Unix socket at
`/run/traffic-control/finish.sock`; no shell or TCP control endpoint is involved.
Do not manually replay these calls. Request/ack/receipt files are fsynced mode
0600 within a mode-0700 directory; never reuse a boundary for another action.

Preserve `request.json`, `ack.json`, `receipt.json`, all ordered segment JSONL,
`completion.json`, `summary.json` and hashes. Verify request IDs and exact roster
match, every receipt client is acknowledged, every observed process has retained
`final=true` evidence/`traffic_final`, Scheduled = Attempts + Skipped and Connected
= 0, and no new finish-induced skips occurred. Reassess the ordered hashed manifest;
a helper acknowledgement alone never establishes PASS. Both finals and receipt
must arrive within five seconds of the boundary, including frames where only
one process is final. Missing/late finals, changed identities, uncertain exec,
missed polls or incomplete history yield INCONCLUSIVE; observed SQL failure,
timeout or ambiguity retains FAIL precedence.

The two-minute final hold keeps HTTP evidence/metrics and `/health` alive while
`/ready` becomes false after sessions close. It does not extend settling. Capture
must close and final evidence be durable before either process disappears.
After the hold, Kubernetes restores the GitOps-owned two replicas automatically.
Verify two new Ready process identities, live image digests and healthy SQL/metrics;
record restoration separately, outside the closed window. Do not scale down or
race Argo self-heal. A restart/deletion before durable final capture makes the
window INCONCLUSIVE; restored processes cannot replace missing finals.

Unknown finish outcome is never retried: retain evidence and sanitized receipt,
read exact `/evidence` plus Pod/container identities, stop new replacement
mutations, allow bounded restoration and reconcile the observed fleet. If clients
do not restore, stop at reviewed recovery. Keep the flag false during disabled
proof; during full-run faults restore it only after active work settles through
the guarded rollout below. The disabled path has a dated observed proof;
full execution remains blocked until fresh provider/cost/time/credential and
action-time gates are satisfied, including explicit direction to change the
currently false replacement flag. Approval of this runbook authorizes no
runtime operation.

## Reviewed recovery for a stale finish socket

A SIGKILL or OOM can leave `finish.sock` in the Pod's memory-backed `emptyDir`.
That volume survives a container restart in the same Pod, so the new process
fails closed when binding the existing socket and may enter CrashLoopBackOff.
Do not unlink an unknown socket or retry finish against a changed process.

If this occurs, preserve the exact Pod UID/container/process history and available
final evidence, mark any window missing durable finals INCONCLUSIVE (retain SQL
FAIL precedence), and stop new replacement mutations. Review Pod termination
reason, restart count and current fleet before recovery. After separate approval
of the exact affected Pod replacement, let its owning Deployment recreate that
Pod with a fresh memory volume; keep desired replicas at two and Argo self-heal.
Do not delete a Pod before its final evidence is durable unless explicitly approved
as fault recovery with the resulting evidence loss recorded. Verify two Ready
new processes, the reviewed image IDs and healthy SQL/metrics afterward. This
recovery invalidates the interrupted proof; start a new authorized capture.
Automatic restoration after a normal controlled exit and stale-socket recovery
remain live qualification gates; offline tests do not establish either outcome.

## Conditional replacement sequence after disabled proof and action-time gates

### Worker control while provider feed is active

The local Workbench CLI can stall a simultaneous provider feed when a second
`exec` opens a worker shell. Before starting the full capture, stage the reviewed
`worker_control.py`, the verified guard binary, and `finish_clients.py` in a
private mode-0700 worker run directory. Start one detached worker control process
per fixed action (`enable`, `finish`, `disable`), each bound to the exact run ID,
SHA-256 of its one expected trigger, fixed cluster/group/proxy/region, and a
maximum 7,200-second wait. Start all watchers shortly before the collector;
the extra time covers setup, the collector's 5,400-second maximum and cleanup.
Download each private `control/ACTION.heartbeat.json` before its trigger and
require the exact run ID/action plus a timestamp no more than 15 seconds old;
if clocks disagree or the heartbeat is stale, stop and inspect without upload.
Use separate `commands/` and `control/` directories, both mode 0700. The worker
accepts an owned, single-link trigger at mode 0600 or Workbench's observed mode
0644 inside that private directory. Workbench CLI **upload**
delivers the exact one-use JSON trigger; Workbench CLI **download** retrieves the
sanitized status receipt. Do not issue another Workbench `exec` while the feed
and capture are active. Test a `probe` trigger and receipt with the provider feed
running before using the action processes.

The control helper claims a trigger before any action and refuses a duplicate,
changed digest, scope or action. Consequential actions share a nonblocking
worker lock. `enable` and `disable` each call the existing guard once, persist
its sanitized set receipt before requesting rollout, then require rollout and
effective-config verification on a Ready new Pod. A set, rollout or verify error
is `INCONCLUSIVE`; inspect live state privately and do not repeat or reverse an
uncertain mutation automatically. `finish` calls the existing exact-identity
client helper once for `full-boundary` and reports only its sanitized
acknowledgement. A client error is `INCONCLUSIVE` and needs identity/evidence
reconciliation. Keep trigger and receipts out of Git and the public repository;
remove them with the temporary run directory after evidence retention and
credential cleanup. The helper does not authorize a flag change or finish by
itself: run the existing action-time gates before delivering each trigger.
If no status receipt arrives after an upload, classify the action as uncertain;
never reupload that trigger or assume the mutation did not happen.

1. Recheck every gate and obtain exact action-time authorization. Start a **new**
   full collector with `--boundary-dir` as above at 5,400 seconds maximum before flag enablement, with the provider
   feed already healthy. One process/clock spans rollout, both approvals and client
   restarts; rotation never resets counters or identity. Do not splice clean windows.
2. With producers quiesced and no unsettled work, run the hashed worker guard:

   ```sh
   gateway-live-gate --namespace raptor-system --secret gateway-live-config --expected false --set true
   gateway-live-gate rollout --expected true --resource-version RECEIPT_VERSION --changed-after RECEIPT_TIME
   gateway-live-gate verify --expected true --resource-version RECEIPT_VERSION --changed-after RECEIPT_TIME
   ```

   Use exact sanitized receipt version/time returned by set. No Secret bytes or
   snapshot input. Conflict, unknown response, mismatched scope/version, old Pod,
   effective config mismatch or failed rollout blocks new requests; reconcile
   privately before any further mutation. Do not retry a possibly committed change.
   Verification checks effective loaded config, owner chain, new Pod UID and readiness.
3. Resume only authorized producers to create **one new immutable** signed-in
   `db-proxy.replace-nodes` request, `executionMode: replacement`, model
   `gpt-6-luna`, pinned `skills-v0.4.0`, resolved exact scope. Record request ID,
   definition SHA and skills commit; keep identical across all resumes. Prior
   qualification `87b0ba57-a6d3-487a-96e7-530426e35878` cannot be converted/reused.
4. Agent records/protects originals; expand once 2→4; qualify both new nodes with
   healthy NLB registration and SQL probes; protect new nodes. Deregister/drain
   only first original, restart only the bound client Deployment, require fresh
   complete zero active/idle/waiting for every reported pool/user on that original.
5. First distinct approval binds this request, action ID A, `ess.scale-in`,
   `rdev.ali`, exact DP00001/group and `desiredCapacity: 3` (observed 4→3).
   Verify completed checkpointed pause and human decision; resume same immutable
   binding, gather fresh evidence, submit A once. Record provider acknowledgement
   separately from convergence. Denial or stale gate ends that action.
6. Authoritatively reconcile 3 before draining second original. Repeat fresh drain,
   bound client restart and complete zero gate. Require a **different** action ID B
   and approval ID, exact `desiredCapacity: 2` (3→2), separate checkpointed pause,
   verified human decision, same immutable request on resume and new evidence.
   Approval A and chat text never authorize B. Submit B once.
7. Unknown/submitted acknowledgement is not convergence. Never replay it or invent
   another action ID. Read authoritative desired capacity, complete ESS membership
   and protection, NLB registration/health, deployment, SQL and metrics. Unresolved
   state stops scale-in, preserves observed fleet and requires reviewed recovery;
   do not automatically reverse scaling. Preserve retained healthy node protection.
8. Convergence requires desired two, both originals absent, two distinct new
   protected healthy registered nodes, successful SQL and bound client readiness.
   Model result stays `acceptance: pending`. Use the separately reviewed stop and
   restore sequence above to close the traffic window; Task 4 proof and action-time gates remain required.
9. Reassess the original ordered manifest on a private copy:

   ```sh
   PYTHONPATH=test-assets/traffic python3 -c 'from pathlib import Path; from segmented_capture import assess_manifest; import json; print(json.dumps(assess_manifest(Path("/PRIVATE/full-capture"))))'
   ```

   PASS requires every observed process final marker, covered successful operations,
   scheduling/roster continuity, complete fresh inventory/metrics, no >3-second poll
   gap, settled boundaries and complete hashed segments. Failed/timed-out/ambiguous
   SQL gives FAIL. Missing lifecycle/coverage/skips gives INCONCLUSIVE. A later clean
   segment or model answer cannot upgrade either result. Overall acceptance also
   needs exact approval/control evidence and cleanup, not traffic status alone.

## Settlement, disable and cleanup on every path

On fault or cutoff, stop new mutations; record exact desired capacity, members,
protection, backends and traffic state. Keep evidence collection within its bound.
Unknown active work must settle or enter separately reviewed recovery before rollout.
Do not clear the flag and restart Gateway through an active attempt to force cleanup.

After attempt settlement and producer quiescence, restore with the same guard:

```sh
gateway-live-gate --namespace raptor-system --secret gateway-live-config --expected true --set false
gateway-live-gate rollout --expected false --resource-version RESTORE_VERSION --changed-after RESTORE_TIME
gateway-live-gate verify --expected false --resource-version RESTORE_VERSION --changed-after RESTORE_TIME
```

Use actual restoration receipt. If still false before any enable, verify current
nonsecret flag/runtime state without forcing a transition. Failed/unknown set or
rollout needs reconciliation; never claim disable succeeded from an attempted command.
Confirm checkpoint status, absence of attempt sandbox and owned keys, revoked
attempt access and final client/fleet state. No blind retry or automatic fleet repair.

Stop the owned feed and collector, retain only allowlisted evidence, completion,
hashes and sanitized receipt in the **existing** private encrypted OSS evidence
prefix with verified object encryption and bounded lifecycle/expiry. Record exact
private retention reference in private receipt; do not invent a bucket or retention
duration. Verify stored hashes before deleting local/worker run evidence. Storage
failure leaves private evidence available for reviewed retry, never deletes it first.

Remove kubeconfigs independently of evidence-upload success, cancellation, failed
preflight, partial upload, denied approval or recovery. Use exact unique paths
chosen at authorization, e.g. local `rm -f -- /private/tmp/UNIQUE_RUN/kubeconfig`;
worker via approved Workbench `exec -P infra-ops-poc -r ap-southeast-1
-i i-t4nj1bitmcz7cuoch82h -c 'rm -f -- /root/UNIQUE_RUN/kubeconfig'`.
Verify absence on each host without printing content. Delete authorized transient
run files after verified retention, never recursive deletion of unrelated directories.
If worker unreachable, mark remote deletion UNKNOWN, revoke/expire temporary access
through the approved path and track cleanup to resolution. Do not record success.

## Sanitized receipt schema (version 1)

This is an allowlist contract for an operator-produced JSON receipt, not an implemented
validator. All objects reject additional fields; missing observations use `null`,
never invented values. Times are UTC RFC3339; hashes are lowercase SHA-256 hex;
Git commits are full resolved 40/64-character hex; IDs are bounded nonsecret strings.
No raw command output, free-form logs, Secret payloads, SQL, DSN, token, credentials,
private addresses or kubeconfig content. Review before public sharing.

| Object | Required fields and types |
|---|---|
| root | `schemaVersion: 1`, `runId: string`, `startedAt/endAt: timestamp|null`, `scope`, `artifacts`, `request`, `approvals: array`, `fleetBefore/fleetAfter`, `captures: array`, `assessment`, `cleanup`, `retention` |
| scope | `envCode: rdev.ali`, `proxyCode: DP00001`, `targetDbCode: D00002`, `applicationCode: A00003`, `groupId`, `serverGroupId`, `clusterId`, `workerId`, `applicationUid`: string |
| artifacts | `sourceCommit`, `skillsTag: skills-v0.4.0`, `skillsCommit`, `skillSha256`, `guardBinarySha256`: string|null; `sourceFiles/workerFiles: array<{relativePath:string,sha256}>`; `images: array<{component:string,digest:string,liveImageId:string|null}>` |
| request | `requestId`, `definitionSha256`, `selectedSkillCommit`: string|null; `model: gpt-6-luna`, `executionMode: replacement`, `convergence: true|false|null`, `acceptance: pending` |
| each approval | `actionId`, `approvalId`, `requestId`, `definitionSha256`, `bindingDigest`: string; `fromDesired/toDesired: integer`, `decision: approved|denied|pending|unknown`, `pausedAt/resumedAt/decidedAt: timestamp|null`, `submission: not_submitted|submitted|unknown`, `providerRequestId: string|null` |
| fleet snapshot | `observedAt: timestamp|null`, `desired: integer|null`, `members: array<{instanceId:string,protected:boolean|null,healthy:boolean|null,registered:boolean|null}>` |
| each capture | `kind: disabled-path|full-window`, `manifestRelativePath: string`, `manifestSha256`, `segments: array<{relativePath:string,sha256}>`, `pathProof: complete|incomplete|unknown`, `observedProcesses: array<{podUid:string,processId:string,finalObserved:boolean}>`, `status: PASS|FAIL|INCONCLUSIVE`, `reasonCodes: array<string>` |
| assessment | `status: PASS|FAIL|INCONCLUSIVE|NOT_RUN`, `controlEvidenceComplete: boolean`, `clientFinalization: blocked|complete|incomplete|not_run`, `reasonCodes: array<string>` |
| cleanup | `flagFalseVerified`, `checkpointSettled`, `sandboxAbsent`, `keysAbsent`, `accessRevoked`, `clientsRestored`, `localKubeconfigAbsent`, `workerKubeconfigAbsent`, `runFilesRemoved`: true|false|null; `verifiedAt: timestamp|null` |
| retention | `encryptedVerified: boolean|null`, `hashesVerified: boolean|null`, `expiresAt: timestamp|null`, `privateReference: string|null` |

For a real full run require two approvals with distinct action/approval IDs and
ordered (4,3),(3,2) transitions, matching request/definition; no pending/unknown
approval counts as permission. Bind hashes to actual artifacts. Overall PASS
requires full capture PASS, control evidence complete, finalization complete,
verified retention and all applicable cleanup checks true. A disabled-path capture
can never establish replacement acceptance. A blocked or unexecuted run records
NOT_RUN, not PASS. This schema's `processId` is the collector's process identity,
not a guessed Pod name; `null` cleanup checks remain unresolved.

## Existing regression evidence and verification receipt (2026-10-16)

No contract gap found; no new fixture or production correction. RED is not applicable
because tests were not added. Generic exact-binding cases apply to both desired
values, so do not duplicate them merely with node names changed:

- `approval-pause.test.mjs`: “approval pause waits for completed tool round and binds
  exact action”; “foreign or altered approval cannot become a wait” (action/desired
  swap rejection); “approved receipt is still a pause and never a scale-in execution
  instruction”; “pending approval blocks mutations before another tool in the same round”.
- `operation-profile.test.mjs`: “approval selector verifies full exact target and parameters”.
- `replacement-trace.test.mjs`: “trace survives a verified session reopen and refuses
  submitted action replay” (unknown acknowledgement retained, definition tamper rejected).
- `replacement-mcp.test.mjs`: “replacement MCP accepts proxy identity and preserves unknown acknowledgement”.
- `replacement-completion.test.mjs`: “fixed observer qualifies convergence without
  certifying retained traffic acceptance” (2→4→3→2, final unknown acknowledgement,
  reconciled observer and `acceptance: pending`); “missing stages unresolved submissions
  old nodes and stale health cannot qualify”.
- Infra `TestApprovalCheckExactBindingAndFailClosed`, `TestSubmissionClientExactWireAndAuth`,
  `TestSubmissionClientFailsClosed`, `TestScaleInGatesAndLostReplayClaimFailClosed`,
  `TestSubmissionAndReceiptFailuresRemainUnknown` cover authenticated check/claim/receipt boundaries.

Commands below were actually run locally against current interfaces. These are offline
fixtures, not cloud, image or traffic acceptance:

| Command (directory relative to repository) | Observed result |
|---|---|
| `node --test approval-pause.test.mjs replacement-completion.test.mjs` (`components/agent-harness`) | PASS 7, no skips |
| `node --test` (same) | PASS 126, no skips |
| `GOCACHE=/tmp/pgcat-task5-go-cache go test ./internal/approval` (`components/infra-api`) | PASS; loopback TLS fixtures required sandbox escalation, no credentials/cloud |
| same cache `go test ./internal/commands ./internal/policy` (included in broader invocation) | PASS both packages |
| same cache `go test ./internal/provider` (included in broader invocation) | BLOCKED setup: missing cached Alibaba/Kubernetes modules, default module-cache writes denied; not a contract failure |
| same cache `go test ./...` (`tools/gateway-live-gate`) | PASS |
| `python3 -m unittest discover -s test-assets/traffic -p 'test_*.py'` | PASS 75 |
| same discovery in `test-assets/metrics` | PASS 2 |
| same discovery in `scripts/gitops` | PASS 24 |
| discovery in `scripts/rdev` with `test_*packaging.py`, `test_proxy_runtime_package.py`, `test_platform_release.py` | PASS 2, 1, 14 respectively |
| `helm template rdev-test-client helm-chart/infra-test-client -f infra-kubernetes/environments/rdev.ali/test/client-values.yaml` | PASS render |
| `helm template rdev-test-metrics helm-chart/infra-test-metrics -f infra-kubernetes/environments/rdev.ali/test/metrics-values.yaml` | PASS render |
| `helm template rdev-platform helm-chart/raptor-platform -f infra-kubernetes/environments/rdev.ali/values.yaml` | PASS render |
| `scripts/rdev/check-platform-image.sh PINNED_IMAGE gateway` | NOT RUN: Docker unavailable, no built image inspected |

Required before live execution: verify Task 4 disabled-mode finalization path; run remaining affected
Go/provider and actual pinned image acceptance in a suitable environment; review/deploy
through existing workflow; verify live hashes, fresh preflight and disabled path proof;
then obtain each action-time authorization. No live action was performed by Task 5.


## Source-bound client release preparation receipt (2026-10-16)

PR [#230](https://github.com/jamesDeng/raptor-iap/pull/230) merged. GitHub Actions
[run 38880780178](https://github.com/jamesDeng/raptor-iap/actions/runs/38880780178)
succeeded for check and publish at source commit
`820fe26eefe1dcf417c60f5d78adcc19cf1456f9`. The downloaded immutable publication
receipt binds that source to
`ghcr.io/jamesdeng/raptor-iap-test-client@sha256:c4ead1a23ced74893b10a3cab753c3828cab88512dea176ba4e5ae83cbdb178a`.
This release preparation pins the rdev test client values to that digest for GitOps
review. The pin does not establish deployment or image acceptance.

The selected skill release is `skills-v0.4.0`, commit
`5a5bb17684956b3050aa1a578e8a51204e06f6c9`, with skill SHA-256
`ad75dc5083db9226bfe5adc2106053f4f8dd23162c6398220cf41a852cf70608`.
The worker was reachable and reported x86_64, Python and kubectl availability;
default kubectl has no context. These are preparation observations supplied to
this receipt, not a fresh cluster preflight or proof of the live runtime.

Live image identity, runtime flag state, disabled-mode finish behavior and retained
traffic acceptance have not been proven. No live execution or traffic acceptance
is claimed by this release preparation; full-run assessment remains NOT_RUN.
