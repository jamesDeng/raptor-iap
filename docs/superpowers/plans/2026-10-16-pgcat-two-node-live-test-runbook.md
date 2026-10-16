# PgCat two-node live-test operator runbook

Status: offline preparation only; **full execution BLOCKED** by client finalization below.
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
peak, runtime and retention within RMB 1,000; record observation time and source.
Do not start after 21:30 Asia/Shanghai; stop new mutations by 22:45; reserve
reconciliation/credential deletion before daily 23:59 stop. Defer if the remaining
budget cannot cover the entire capture, approvals and cleanup. Capture maximum is
5,400 seconds, each segment maximum 1,800 seconds, provider refresh 15 seconds.

Verify signed-in Raptor creator/admin and supported replacement model
`gpt-5.6-luna`; qualification model support is not replacement support.
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

## Worker preparation and disabled 60-second path proof

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
`segmented_capture.py`) and the hashed guard binary matching worker architecture.
Record worker SHA-256 readback. Use fresh private output paths; never reuse a capture directory.
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
Publication failure stops the feed; do not refresh freshness from cached inventory.
Age >30 seconds, wrong roster or target discrepancy blocks acceptance. If target
update succeeded but inventory publication failed, reconcile before restarting.

With replacement flag still false, run on the authorized worker with private
`KUBECONFIG` and reviewed modules colocated:

```sh
python3 worker_capture.py --output /root/PRIVATE_RUN/disabled-60s \
  --inventory /root/PRIVATE_RUN/provider-inventory.json --seconds 60
```

Verify direct Ready client Pod UID/process evidence access and private Prometheus
access, one-second continuity, exact provider roster, `up=1` and complete pool/user
active/idle/waiting triplets with DP00001/D00002/rdev.ali labels and raw timestamps
under 45 seconds. Preserve all segment files, `completion.json`, `summary.json` and
SHA-256 hashes. A structurally complete manifest and explicit collector completion
prove the path. **Continuously running clients without final markers yield traffic
INCONCLUSIVE even in this successful 60-second path check. Never label it traffic PASS.**
Missing polls, source or lifecycle evidence must remain visible. Keep the flag false.

## Full-run finalization blocker and required amendment

`helm-chart/infra-test-client/templates/deployment.yaml` hardcodes `replicas: 2`,
RollingUpdate `maxUnavailable: 0` / `maxSurge: 1`, and 20-second termination grace.
`infra-kubernetes/environments/rdev.ali/test/client-application.yaml` enables Argo
self-heal. Manual scale-to-zero can be reverted; rolling restart creates new,
nonfinal processes. Collector timeout does not terminate client scheduling.
Every process observed during the full window, including restarted processes,
requires an explicit retained `traffic_final`. Therefore **do not enable replacement
or start a full run using this runbook until a reviewed implementation task closes
this defect**. There is no reliable supported finalization/restoration command today.

Proposed amendment for review: provide a GitOps-owned bounded measurement stop
that ends scheduling in both current client processes while retaining their HTTP
final evidence endpoints. Prove deadline settlement/session close and capture final
markers within collector settling limits (at most five seconds); preserve prior
restart finals. It must not permit a liveness restart or replacement process before
capture completion. Alternatively a GitOps-owned replica stop must demonstrate
final evidence stays discoverable during terminating/unready Pod lifecycle; the
current Ready discovery and 10-second final endpoint lifetime cannot be assumed
to provide this. Do not use an imperative scale/Argo race.

The new task must supply tested exact stop commands, lifecycle timing, expected
process roster, bounded rollback and GitOps restoration to two Ready clients.
At run end: converge replacement first; separately authorize the controlled stop;
end measurement with continuous roster through its boundary; collect all process
finals during bounded settling; finalize/hash/assess; then perform reviewed restore
and verify 2/2 Ready with healthy SQL/metrics. Restored traffic is outside the closed
window. Record restoration separately; its new processes cannot replace missing
final markers from the measured window. Failure to finalize yields INCONCLUSIVE
(or FAIL for observed failed SQL); preserve evidence and restore through reviewed recovery.

## Conditional replacement sequence after blocker is resolved

1. Recheck every gate and obtain exact action-time authorization. Start a **new**
   full collector at 5,400 seconds maximum before flag enablement, with the provider
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
   `gpt-5.6-luna`, pinned `skills-v0.4.0`, resolved exact scope. Record request ID,
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
   restore sequence above to close the traffic window; this step is currently blocked.
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
| request | `requestId`, `definitionSha256`, `selectedSkillCommit`: string|null; `model: gpt-5.6-luna`, `executionMode: replacement`, `convergence: true|false|null`, `acceptance: pending` |
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

Required before live execution: resolve finalization amendment; run remaining affected
Go/provider and actual pinned image acceptance in a suitable environment; review/deploy
through existing workflow; verify live hashes, fresh preflight and disabled path proof;
then obtain each action-time authorization. No live action was performed by Task 5.
