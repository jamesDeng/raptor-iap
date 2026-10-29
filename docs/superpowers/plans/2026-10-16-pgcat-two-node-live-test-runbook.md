# PgCat two-node live-test operator runbook

## 2026-10-29 owner direction: replace both originals with 2→4→2

The owner explicitly selected this full-run sequence, superseding the earlier
staged 2→4→3→2 design:

1. Expand ESS once from two to four and qualify both replacement nodes.
2. Stop new connections to **both** exact originals; verify their backend
   withdrawal while both replacements remain healthy and registered.
3. Rolling-restart the related, scope-bound application Deployments.
4. Observe fresh complete zero active/idle/waiting client counts across every
   pool/user on **each** original.
5. Scale ESS directly from four to two, retaining exactly the two replacements.

Keep both replacements protected; make only the verified drained originals
eligible for removal. Bind one scale-in action/approval to desiredCapacity 2 and
the immutable request scope. Recheck both zero gates and retained capacity after
any pause before dispatch. Never accept arbitrary two-node fleet convergence.

This direction supersedes the earlier same-Pod/no-restart drain restriction for
future restart-based runs and the proposed private session-rotation mechanism.
It does not change historical verdicts or make current fixed-roster code admit a
restart. A replacement-disabled no-scale native observer proof must test startup,
the same restart behavior, retained evidence and closure before full replacement.
The superseded paid2→3→2 candidate rehearsal is not a prerequisite for the new
strategy. No new session-rotation command is required by this choice.

The reviewed client source/chart now uses RollingUpdate, maxUnavailable 0,
maxSurge 1 and a 30-second termination grace period, with accepted SQL jobs
draining under one five-second reconnect-plus-operation budget. Source tests
and task review passed; this does not establish an actual deployed rollout.
Verify bounded graceful draining and retain final evidence for every retiring
Pod/process, plus exact successor identities and advancing SQL, in the same
continuous capture. Never reset counters, discard failures or splice windows to
accommodate the rollout. Keep the Deployment UID and reviewed image binding;
record the authorized template/generation and ReplicaSet transition explicitly.

After rollout, require two distinct fresh complete zero active/idle/waiting client
scrapes across every pool/user on each withdrawn PgCat node and fresh provider
absence evidence before scale-in. A completed rollout or restart receipt is
not a zero-connection proof. Missing final evidence, remaining connections or an
uncertain restart blocks scale-in; observed SQL failures remain FAIL.

Status: the capacity guard, direct native result/skill contract, topology assessor
and graceful shutdown source changes are implemented, tested and task-reviewed.
They have not established deployment or live replacement readiness. The old
strict-majority-only guard and staged 4→3→2 result contract motivated the update;
the new source has a narrow protected-pair exception and version2 direct contract.
Do not bypass source/runtime admission or mutate immutable skills-v0.4.0.
Native observer rolling-cohort admission/closure and a fresh disabled proof remain
required. No live restart or cloud operation was executed by this amendment.

## Earlier verification and implementation status

2026-10-28 update: reviewed merge `7508dfac` completed a tracked no-scale timing
**PASS**, independently reassessed from retained capture and sealed records:
64.003 seconds measurement, 66 frames, maximum gap 1.002616 seconds and 195 SQL
successes per client. Exact feed/supervisor exits, both credential absences and
the finished reservation release were verified. This proves only that source's
no-scale timing path. Ordinary transition assessment is INCONCLUSIVE; the paid
rehearsal and full replacement remain unproven. Paid admission remains
structurally disabled pending the long collector and Task 6D close/abort path.
See project-root `working/handoffs/2026-10-28-pgcat-live.md` for evidence hashes.

Status as of 2026-10-26: request `570533ed-e417-4bc5-beba-ad4db3db8d20`
ended **failed/ToolFailed** before approval or scale-in. Its retained full
capture is **INCONCLUSIVE** (`missing metrics coverage`), not replacement PASS.
Reviewed staged recovery returned to desired two, exactly the two protected
healthy originals, with Gateway `replacementEnabled=false` and idle; recovery
observed SQL timeouts and skipped schedules around deregistration and restart.
The 2026-10-18 disabled finish/restoration PASS (62 frames, 384 successful
operations) remains a historical proof of that window only. **A new full
request is BLOCKED** until the scale-out readiness changes are reviewed,
applied and verified, a new disabled capture and bounded 2→3 rehearsal pass,
and all separate action-time gates are met. Local source changes do not
establish live installation or rehearsal success.
Historical status source: project-root
`working/handoffs/2026-10-26-pgcat-live.md`, “Full two-node run in progress”
and “Staged recovery settled, 14:26 Shanghai”.
This document authorizes no credential transfer, Secret change, rollout or replacement.
Use the approved [design](../specs/2026-10-16-pgcat-two-node-live-test-design.md),
existing `agent-skills/pgcat-replacement/SKILL.md` two-node strategy, and
`test-assets/traffic/README.md` interfaces. Historical receipts are not current preflight.
The first 2026-10-26 disabled 2→3→2 rehearsal also remains **INCONCLUSIVE**: its
private `traffic-evidence/pgcat-scaleout-20261026-1627/rehearsal-inconclusive-evidence.tgz`
contains 1,189 frames and 7,134 successful SQL operations with no observed
Failure/Timeout/Ambiguous/Skipped, but strict metric, roster, poll and provider
freshness gaps. It did not unlock a full request. Follow the reviewed
[continuity design](../specs/2026-10-26-pgcat-rehearsal-continuity-design.md) for a
new capture; never relabel or splice the historical window.

## Hard gates and action-time authorization

Scope: `rdev.ali`, `DP00001`, `D00002`, `A00003`, Singapore `ap-southeast-1`,
ESS `asg-t4necb4dfmu6o3z2rn0g`, NLB group `sgp-xs5s42tihtl6ja2a67`,
ACK `c92787e953503492ea141a744c81498f1`, worker `i-t4nj1bitmcz7cuoch82h`.
Rediscover exact scope and Deployment UID; mismatches require review, not editing a guard constant.
Expected originals at design time: `i-t4n5kisq7u820wbx8nri`, `i-t4n5kisq7u820wbx8nrj`.
The 2026-10-26 recovery ended on originals `i-t4n8ofwrovy3mv198ela` and
`i-t4n8ofwrovy3mv198elb`; rediscover and verify both actual baseline originals
before any action. Historical IDs are not a substitute for current provider reads.

Check Alibaba billing and available balance once per calendar day before that
day's first live action. Present the day's cost/balance observation and forecast,
including four-node peak, runtime and retention within the owner-approved
ceiling; record observation time and source. Reuse that observation for further
actions that day rather than querying billing repeatedly. The original ceiling
was RMB 1,000. On 2026-10-18 the owner
raised the whole-POC ceiling to CNY 1,200 net pretax for this one test after
the billed October net reached CNY 959.08. On 2026-10-19 the owner raised the
ceiling to CNY 1,500 net while emphasizing affordability and careful spending.
On 2026-10-25 the owner raised the whole-POC net ceiling to CNY 1,800,
while asking for a clear progress summary and continued cost care.
This is a stop limit, not a spending target. These amendments do not remove the
daily forecast, balance, delayed-billing, or stop gates.
Do not start after 21:30 Asia/Shanghai; stop POC work and hand off by the owner's
22:30 cutoff, including reconciliation and credential deletion. The existing
daily resource shutdown remains scheduled for 23:59. Defer if the remaining
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

## Transition evidence release gate (2026-10-25)

The recorded replacement request `0bad9b07-625e-42cb-a021-257775e08930`
was cancelled and settled after its provider feed stopped; its retained result
remains **INCONCLUSIVE**. Do not reopen that request or reinterpret its historical
schema-1 archive. Keep `replacementEnabled=false` while preparing this release;
a current readback is required before any later live action. Local synthetic
traffic PASS does not establish overall replacement acceptance.

Historical provider/collector review covered the partial multi-Pod evidence fix
at `8094452784cfa4969c1cb2648e484369389b277c`. The updated hashes below bind the
current disabled-rehearsal continuity implementation, bound-roster finish
extension and other worker helpers;
the old commit no longer binds these changed bytes. This is local source verification,
not a live installation or traffic proof.
Before release, require whole-branch independent review, passing CI and merge;
record the final resolved release commit and confirm these files match its tree.
If any source bytes change, repeat review and verification and replace the
binding before use. A hash match alone does not prove worker installation.

| Source under `test-assets/traffic/` | SHA-256 |
|---|---|
| `provider_feed.py` (local operator) | `2570a869330d4e1267f1e89b76f93801084a18c1af6a6e70e8799a3cd6ccac28` |
| `worker_capture.py` | `167a07649e6756bfd914e7d01f8507c686ced712937873813d43282aa394e3dd` |
| `capture_window.py` | `99a61e55a561972164f0216a0b2fb351b0deda9bca68aad0a0115a0af3f3e988` |
| `segmented_capture.py` | `2ad61819b59ff8ef10e1de3873029eecd349a920b734c8b480e8dcfbae5f7411` |
| `finish_clients.py` | `19b10c4d237d5a3767c4d59075c51f0e5e3057e141d35d88a1eee6723475ec96` |
| `worker_control.py` | `9e9018d4d1072607adbaef0fd00a730582548b14f8a4a3034e1b21d2fcd5ce9e` |

Verify local provider bytes and read back every staged worker module's SHA-256
before launching either process. Keep a per-file worker bundle manifest in the
private receipt, together with the existing guard binary and image bindings.
Require a fresh replacement-disabled capture using schema 2, exact two-client
finalization and verified 2/2 restoration before considering a new paid run.
The older disabled-mode proof does not qualify this changed feed/collector.

Recheck the one-operator, time and cost gates before that later run. The
2026-10-25 billing observation is already complete: reuse its recorded balance,
cost and forecast rather than querying billing again that day. A subsequent
calendar day needs its own daily observation. The CNY 1,800 net whole-POC ceiling,
21:30 Shanghai live-start cutoff and 22:30 handoff including cleanup remain
binding. Defer even before 21:30 if approvals, full capture, reconciliation,
retention and credential deletion cannot finish by 22:30. This local release
preparation authorizes no paid attempt, flag change or credential work.

## Disabled scale-out readiness rehearsal after reviewed hook apply

Follow the committed
[scale-out readiness design](../specs/2026-10-26-pgcat-scaleout-readiness-design.md),
“Runtime contract” and “Verification and rollout”, for this additional gate.
Before another paid rehearsal, require review, CI, merge and a fresh
replacement-disabled **no-scale long feed/collector check** with the local
publication receipt, no overlapping Workbench status calls, exact client finish,
independent retained-archive assessment, verified 2/2 restoration and cleanup.
That check must verify the bounded post-feed worker validation/finish sequence
described below; local fixtures alone cannot qualify its timing. Recheck the one-operator, exact baseline,
cost and time gates before any subsequent paid action.

This is a separate, bounded **2→3→2** plumbing and timing proof, not the native
2→4→3→2 replacement request. First require the sanitized Terraform PR plan
comment, reviewed apply and authoritative readback of exactly one PgCat-owned ESS
`SCALE_OUT` lifecycle hook on group `asg-t4necb4dfmu6o3z2rn0g`, with
`heartbeat_timeout=420` seconds and `default_result=CONTINUE`. Record its actual
provider hook ID and configuration. Verify existing ESS/NLB addresses, state
ownership and two-node baseline are unchanged. The operator neither acquires nor
completes a lifecycle token. A disabled capture without scale-out checks feed,
collector and client plumbing only; it does **not** prove Pending target timing.

Obtain separate action-time authorization for this exact one-node rehearsal and
its separately reviewed recovery. At its start, require one operator; Gateway
`replacementEnabled=false`, a Ready rollout, `idle=true` and no AX actor; exact
two rediscovered Protected/Healthy originals and desired two/min two/max four;
both NLB backends Available/Normal with weight 100; client Deployment 2/2 Ready
with two distinct clean SQL processes and zero Failure/Timeout/Ambiguous/Skipped;
metrics Deployment 1/1 Ready, fresh successful scrapes and complete raw
active/idle/waiting triplets for both originals; and no unresolved request,
approval, provider activity, capture or target reconciliation. Use the day's
existing billing observation (the 2026-10-26 09:50 Shanghai observation if
acting that day), including delayed charges and remaining headroom under the
**CNY 1,800 net whole-POC cap**. Do not query billing a second time that day.
Do not start a paid action after **21:30 Asia/Shanghai**; defer sooner unless
observation, staged recovery, evidence retention and credential deletion can
finish by the **22:30 handoff**. Record an absolute rehearsal stop deadline
within those limits and the 5,400-second capture cap before starting.

Use the reviewed local Alibaba CLI profile `infra-ops-poc`, region
`ap-southeast-1`, account/ESS group/NLB group and ACK worker scope above.
Read project-root `working/skills/ack-workbench-cli/SKILL.md` before any
Workbench call; use only exact `-P infra-ops-poc -r ap-southeast-1
-i i-t4nj1bitmcz7cuoch82h`. Verify the current CLI argument shape with a
no-network dry run and retain a sanitized action record. With a fresh private
run directory, approved temporary private kubeconfig and checked source/worker
hashes, start **one** provider feed and **one** detached continuous collector
before any scale action. Start the provider with **`--mode disabled-rehearsal`** and
`--status-file /absolute/private-run/provider-status.json`; the Mac-side status
path must be initially absent in an existing owner-only mode-0700 directory.
The feed writes the receipt atomically as mode 0600 only after a successful
worker publication, within the same absolute 15-second refresh deadline. Its
only fields are `feedSession`, `sequence`, `mode`, `desired` and `observedAt`;
it is address-free operational liveness, never retained acceptance evidence.
Keep its adjacent `capture_window.py` dependency at the reviewed hash. This
schema-2 mode binds the initial two original IDs and exact 2→3→2 path for the
whole feed session, capture segments and private archive reassessment. An absent
mode retains historical full-replacement semantics and cannot qualify this
rehearsal. A no-scale capture uses the default mode and is not a rehearsal PASS.
Require first fresh schema-2 provider publication with this mode,
exact two target ConfigMap readback and clean client/metric frames. Preserve
one mode, feed session, sequence and capture throughout; never splice or restart an
uncertain process. Keep the Gateway flag false throughout.

Submit **one non-retried** ESS `ModifyScalingGroup` desired-capacity-three
action for the exact group, with the reviewed CLI profile and scope. Preserve
the exact provider request/activity acknowledgement separately from observed
convergence. If its acknowledgement is missing or uncertain, stop mutations;
read authoritative provider state and obtain separately reviewed recovery.
Never issue the action again under a new or reused identifier. During the
420-second startup hold, retain timestamps and evidence for the new exact ESS
instance ID and private ECS IP in `Pending`/`Pending:Wait`, ConfigMap target
publication with resourceVersion and exact target readback, then Prometheus
`up=1` plus **fresh complete raw active/idle/waiting triplets** for that node
before its first NLB backend registration. Compare provider, target, scrape and
NLB observations on the same clocked capture timeline. After registration,
require Available/Normal NLB health and complete fresh metrics for **every**
backend throughout the retained window; transitional Adding/Unhealthy backends
do not get a metric exception. Continue checking two clean SQL clients, fresh
source ages, sequence and every-backend coverage under the existing assessor.
The hook's automatic `CONTINUE` after 420 seconds is not a readiness signal.

If the hold expires before fresh scrape/triplet readiness, a Pending node enters
NLB without complete metrics, NLB health fails, a source or action becomes
uncertain, or the deadline is reached, stop new actions and label the observed
window **INCONCLUSIVE** or **FAIL** under the strict assessor. Preserve evidence,
use separately reviewed recovery, and block the full request. Do not extend the
hold, relax the collector, repeat an unknown mutation or treat a later healthy
sample as repair of the gap.

After the new node has been observed healthy and the rehearsal evidence is
durable, restore **the exact two protected originals** under the separately
reviewed staged pattern. Freshly prove the new ID is the sole unprotected ESS
member and that the originals will remain two healthy registered backends,
which is a strict majority of three. Deregister only that new ID from NLB once;
retain its provider request/job acknowledgement and verify it is absent while
both originals remain healthy.

Use the **no-restart path** only after all of these operator gates are verified
inside the same continuous capture:

- Exactly the same two client Pod UIDs, container IDs and process identities as
  the bound starting roster remain running, with advancing successful SQL from
  both and zero Failure/Timeout/Ambiguous/Skipped throughout. Preserve the
  unchanged Deployment UID and generation; never restart or roll out clients
  during this assessed rehearsal.
- For the deregistered new node, observe **two distinct fresh complete zero
  samples**, with distinct advancing scrape/raw source timestamps, for active,
  idle and waiting in **every** reported pool/user, including `test`/`poc_app`.
  Each sample needs fresh `up=1`, complete coverage and raw/scrape age no more
  than 45 seconds. Repeated collector frames of one scrape are one sample.
- Retain a later fresh provider publication in the same session with a higher
  sequence and observation time than the second zero sample, showing that exact
  new node absent from **all** NLB backend states while both originals remain
  healthy. Same-publication absence cannot qualify. Every current backend,
  including Adding/Removing/Unhealthy or zero weight, always needs fresh
  complete metrics. Three-second poll and 30-second provider limits still apply.

These two-sample and unchanged-identity gates require action-time operator
verification; the assessor's narrower metric-retirement rule and support for
legitimate finalized client handoffs do not enforce the whole no-restart gate.
If any connection remains, a client identity or process changes, a sample is
stale/incomplete, SQL stops advancing, an adverse counter appears, or an action
is uncertain, stop new mutations and close the assessed window with its actual
strict result. Record the failed operator gate; an assessor PASS alone cannot
qualify that interrupted rehearsal. Preserve evidence, block the full request
and use separately reviewed recovery **outside** that window. A recovery client restart, if separately
authorized, cannot manufacture a rehearsal PASS; never splice recovery frames
or restart a feed into this capture.

Only after all no-restart gates pass, submit **one non-retried** ESS
`ModifyScalingGroup` desired-capacity-two action for the exact group. Preserve
its exact acknowledgement and wait for authoritative desired two, exact
protected original ESS membership, two healthy original NLB backends and new-node
absence in a later fresh provider publication. An unknown deregistration or
scale result is never replayed; stop at reviewed recovery. The assessor may
retire the new node's metrics after complete fresh zero plus later NLB absence
even while ESS still lists it InService/Removing, but a reappearing backend or
later nonzero raw count restores its metric obligation immediately.

While the same provider feed and collector are still running, patch only the
existing `raptor-test/pgcat-provider-inventory` ConfigMap using its live
resourceVersion and the verified final two-node fleet. Read back the exact
ConfigMap name/namespace, resourceVersion and `targets.json`: exactly both
original instance IDs, each verified private address with port 9930 and the
`rdev.ali`/`DP00001`/`D00002` labels, with no new-node or extra target. A patch
acknowledgement or local receipt alone is insufficient. Keep this readback
private. The feed retains its learned target history and skips unchanged target
updates; do not restart it or clear that history during explicit reconciliation.

Record the feed sequence at reconciliation. Then observe a **later** local
success receipt with the same `feedSession`, `mode=disabled-rehearsal`, desired
two, higher sequence and later fresh `observedAt`. The receipt proves only
successful worker publication; it does not prove ConfigMap contents, collector
health or traffic acceptance. Do **not** issue Workbench status polling after
final reconciliation while the feed is active.

Intentionally stop this one local feed and verify that it has exited before
any new worker access. Read its final local receipt after exit; bind its exact
session, sequence and observation time for the next check. Treat an uncertain
in-flight publication, failed stop or changed session as INCONCLUSIVE. Never
restart this feed into the capture. The collector continues on its original
clock and files.

With no concurrent feed, use **one bounded Workbench exec** to perform the
following checks in order, then finish in that same worker call:

1. Read the exact existing ConfigMap again and verify its name/namespace,
   resourceVersion and complete two-original `targets.json`, including exact
   private addresses, port 9930 and scope labels. Keep the readback private.
2. Read a complete, newline-terminated collector JSONL frame recorded **after**
   final reconciliation, from the existing ordered `segment-NNNN.jsonl` files.
   Require the final receipt's exact `feedSession`, `sequence` and `observedAt`,
   `mode=disabled-rehearsal`, desired two, exact original ESS/NLB roster and
   both originals healthy. Require fresh complete `up=1` and every pool/user
   active/idle/waiting triplet for both originals, an unbroken three-second
   poll limit, the same two Pod/process identities with advancing clean SQL and
   zero Failure/Timeout/Ambiguous/Skipped. Compare live Pod/container/process
   identities and Deployment UID/generation with the bound starting roster as
   well; retained sanitized frames alone do not include container IDs. A partial
   last JSONL line, heartbeat, control status or local receipt is no substitute
   for this complete frame. A frame snapshot is provisional until final archive
   hashes, completion and full-window assessment are verified.
3. Only if every check passes and sufficient provider freshness headroom
   remains, invoke **one exact** `finish_clients.py` boundary using this run's
   bound private boundary directory and `--expected-replicas 2`. The new post-stop
   caller **must** pass the original private-contract roster directly as
   `finish(boundary_dir, expected_replicas=2, expected_roster=original_roster)`;
   a CLI caller must supply `--expected-roster-file` pointing to that same roster
   in a private regular nonsymlink mode-0600 JSON file. The JSON is exactly two
   objects with only `name`, `uid`, `containerId`, `processId` (nonempty bounded
   strings, each field unique across the pair); duplicate JSON keys refuse.
   Never derive the expected roster anew from live state. `finish()` validates
   and snapshots the binding, refuses a first-live-read mismatch before writing
   `request.json`, and rechecks the same originals after acknowledgement before
   dispatch. A precheck followed by the legacy unbound call is insufficient.
   Complete the request, both original-process final markers and bounded collector
   settling
   while the last successful provider `observedAt` is **under 30 seconds old**.
   Preserve the existing five-second settling bound. An uncertain finish is
   never replayed.

Prepare and review that bounded validation/finish command before live use; no
new automatic validator is supplied by this source-only change. The bound-roster
finish extension is a prerequisite, not that validator or a timing proof; legacy
unbound helper calls remain available only for historical callers. Existing
`worker_control.py` heartbeat/status files contain control liveness or action
acknowledgements, not collector frames. The existing collector fsyncs each
JSONL frame on the worker. Reading those files after confirmed feed exit avoids
concurrent Workbench feed/status access, but the one call's reads and finish
still consume the remaining **30-second** provider budget. The replacement-
disabled no-scale long check must verify this exact order and timing before
another paid rehearsal; do not assume the worst-case command duration fits.

A stale/missing frame, target discrepancy, client change, uncertain read/action
or insufficient time means no finish claim or qualified rehearsal PASS. Close
INCONCLUSIVE, retain any observed SQL FAIL, and use separately reviewed recovery.
Do not restart the feed, widen freshness, or retry under another action ID.
Retain the single collector's ordered manifest, completion, client process
history, provider acknowledgements, hook configuration and target/metric timing
privately. The assessor still requires the observed one-node expansion and
return to exactly both healthy originals; mode changes, new/changed IDs,
original departure or skipped transitions invalidate the rehearsal.
Assess without suppressing any SQL Failure/Timeout/Ambiguous/Skipped. Such
faults prevent zero-loss PASS even if later replacement processes are clean.
The 2026-10-26 staged recovery recorded timeouts and skips around
deregistration/restart; those historical faults remain visible and are not
evidence that this rehearsal has passed. Verify Gateway false/idle, no active
actor, clean client readiness, private evidence readback and temporary
kubeconfig deletion on worker and Mac. A failed or incomplete rehearsal,
recovery or cleanup blocks any new full request.

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
uncertain first one. For the legacy 60-second no-scale proof and full
replacement, use the reviewed one-use worker control for finish while the feed
is active and check its latest successful publication afterward. The continuity
no-scale long check and disabled 2→3→2 rehearsal follow the explicit post-stop
validation-then-finish sequence above.
The earlier direct provider-feed command is retired. The guarded controller
starts the feed only after its supervisor creates `laneRoot/run-context.json`
from the active journal and startup identity. The feed requires that fixed
context path and the reviewed worker `contractDir` before any worker call.
Direct invocations without this binding refuse at startup.

This reads authoritative ESS/ECS/NLB scope and listener health, atomically publishes
inventory and reconciles only the existing `raptor-test/pgcat-provider-inventory`
ConfigMap targets. It does not acquire credentials or create cloud resources.
The read-only ESS/ECS/NLB calls use Alibaba CLI `--retry-count 2`; any CLI-internal
retries must complete within the same absolute 15-second refresh deadline.
Unrecovered read, timeout or publication failure stops the feed; do not refresh
freshness from cached inventory. Workbench and ConfigMap writes are not retried.
Age >30 seconds, wrong roster or target discrepancy blocks acceptance. If target
update succeeded but inventory publication failed, reconcile before restarting.

Read back the worker inventory and retained collector frames: require
`schemaVersion: 2`, the same UUID `feedSession`, positive `sequence`, fresh
`observedAt`, exact desired capacity, complete `members`/`memberStates` and
`backendStates`, and matching `healthyRegistered`. No private addresses or raw
provider payload may enter the retained evidence. Identical publications may
repeat across one-second frames; a changed publication must advance sequence
by exactly one with a later timestamp in the same session. Restart, skip,
rollback, malformed state or changed content at the same sequence is
INCONCLUSIVE; never splice a new feed into an existing assessed window.

Reconcile monitoring targets before the matching inventory publication. A
changed list requires a resourceVersion-bound ConfigMap patch and exact target
readback within the same absolute 15-second deadline. An unchanged list may use
the existing no-patch path only under the one-operator gate. Keep every previously
observed backend's target through the entire assessed window, including after
its ESS membership or NLB registration disappears. Do not remove an old target
merely because the current desired count dropped.

Every NLB backend requires fresh successful scrape coverage and complete raw
active/idle/waiting triplets for `test`/`poc_app`, regardless of Adding,
Configuring, Removing, weight or listener health. Only a never-ready Pending or
Pending:Wait ESS member absent from NLB may lack metrics/address evidence.
An Adding backend without an exporter scrape makes the full window INCONCLUSIVE.
Retain the old node obligation until a fresh complete zero observation for
**all** reported pools/users is followed by a later fresh provider publication
showing backend absence. A repeated provider snapshot at zero time is not that
later proof. Any later nonzero observation restores the obligation. All-backends
unhealthy is also INCONCLUSIVE. Keep metrics observation age at most 3 seconds,
scrape/raw age at most 45 seconds, provider age at most 30 seconds and client
poll gaps at most 3 seconds; do not widen these limits to obtain PASS.

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
   `gpt-6-luna`, a verified new direct-v2 skill release (candidate
   `skills-v0.5.0`, only after immutable publication), resolved exact scope. Record request ID,
   definition SHA and skills commit; keep identical across all resumes. Prior
   qualification `87b0ba57-a6d3-487a-96e7-530426e35878` cannot be converted/reused.
4. Agent records/protects originals; expand once 2→4; qualify both new nodes with
   healthy NLB registration and SQL probes; protect both new nodes. Under the
   reviewed direct-replacement contract, withdraw both exact originals from new
   connections, restart only the related bound application Deployments, and require
   fresh complete zero active/idle/waiting for every reported pool/user on both
   originals. Only those originals may become eligible for removal.
5. One exact approval binds this request, action ID A, `ess.scale-in`,
   `rdev.ali`, exact DP00001/group and `desiredCapacity: 2` (observed 4→2).
   Verify completed checkpointed pause and human decision; resume same immutable
   binding, gather fresh evidence for both originals and both retained replacements,
   submit A once. Record provider acknowledgement separately from convergence.
   Denial or stale gate ends that action.
6. Observe direct 4→2 convergence. Intermediate membership observations may occur
   during one provider action; they do not authorize another scale action or an
   application restart. Require both originals absent and both exact replacements
   retained, protected, healthy and registered.
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

For the full replacement path, after closing the assessed window, stop the
owned feed and collector before final target reconciliation so a retained-target
feed cannot undo cleanup. The disabled 2→3→2 rehearsal uses its earlier
in-window target reconciliation and readback gate instead.
Under the authorized one-operator scope, read the complete fresh final ESS/NLB
fleet; on successful replacement this must be exactly the two new nodes.
Reconcile `raptor-test/pgcat-provider-inventory` to the exact authoritative final
target list using its current resourceVersion and verify the ConfigMap target
readback. No obsolete old-node target may remain. On a fault, reconcile only to
the verified actual fleet; do not assume the intended two-node result or remove
a still-registered backend. Record unresolved scope or readback as incomplete
cleanup and block overall PASS until reviewed recovery; preserve any observed
SQL FAIL. Record the sanitized authoritative final node IDs, ConfigMap target
IDs, readback time and exact-match outcome in `cleanup.targetReconciliation`
in the private settlement receipt; never retain target addresses.

Retain only allowlisted evidence, completion,
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
| cleanup | `flagFalseVerified`, `checkpointSettled`, `sandboxAbsent`, `keysAbsent`, `accessRevoked`, `clientsRestored`, `localKubeconfigAbsent`, `workerKubeconfigAbsent`, `runFilesRemoved`: true|false|null; `verifiedAt: timestamp|null`; `targetReconciliation` |
| cleanup.targetReconciliation | `observedAt: timestamp|null`, `finalInstanceIds: array<string>|null`, `targetInstanceIds: array<string>|null`, `exactMatchVerified: true|false|null` |
| retention | `encryptedVerified: boolean|null`, `hashesVerified: boolean|null`, `expiresAt: timestamp|null`, `privateReference: string|null` |

`cleanup.targetReconciliation` is required even when verification is unresolved.
`finalInstanceIds` is the complete fresh authoritative final ESS/NLB union;
`targetInstanceIds` is the ConfigMap readback's exact target ID set. Each observed
array must be sorted, unique and contain at most four nonsecret instance IDs;
missing observations remain `null`, not an empty successful result.
`observedAt` records the final target readback time after capture closure and
feed/collector stop. `exactMatchVerified` is true only after the resourceVersion-
bound reconciliation and exact readback described above. Unknown, stale or
mismatched observations cannot set it true. This records IDs and an outcome,
never addresses, raw ConfigMap data or credentials.

For a real full run require two approvals with distinct action/approval IDs and
ordered (4,3),(3,2) transitions, matching request/definition; no pending/unknown
approval counts as permission. Bind hashes to actual artifacts. Overall PASS
requires full capture PASS, control evidence complete, finalization complete,
verified retention and all applicable cleanup checks true, including
`cleanup.targetReconciliation.exactMatchVerified: true`, a non-null post-window
`observedAt`, and equal non-null `finalInstanceIds`/`targetInstanceIds` matching
the two new members in `fleetAfter` with both originals absent. An omitted or
unresolved target reconciliation blocks overall PASS; preserve SQL FAIL
precedence. Historical receipts without this evidence do not satisfy the new
release gate. A disabled-path capture can never establish replacement acceptance.
A blocked or unexecuted run records
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

### Tracked no-scale controller integration (2026-10-27; not live proof)

`test-assets/traffic/pgcat_rehearsal_controller.py` supplies the tracked
`Controller(binding_path)` API, obtained only through the verified entry below.
Ordinary imports are refused at runtime. Integration tests use fake process boundaries;
implementation or an orderly `finished` journal is not a live PASS. Independent
source/binding review and the action-day preflight above remain hard stops.
Keep `replacementEnabled=false`, the historical `RUN_BINDINGS_REVIEWED=False`
and `SIGNAL_LIFECYCLE_REVIEWED=False`; the old detached local pending helper is
not part of this path. No credential staging or live action is authorized by
this documentation update.

Supply a new owner-only mode-0600 JSON manifest in a mode-0700 directory. Its
exact keys are `laneBindings`, `laneRoot`, `captureRoot`, `workerInventory`,
`workerKubeconfig`, and `sourceHashes`; no-scale also requires `localKubeconfig`.
The paid binding omits `localKubeconfig`. Unknown/missing keys refuse launch.
The no-scale local credential must be the regular mode-0600 `kubeconfig` in
`captureRoot`'s private mode-0700 parent, with no symlinked path. Its identity is
retained at startup, and only that exact file may be removed after feed exit.
`laneBindings` follows the exact schema in `pgcat_lane_actions.py`, including
reviewed account, region, worker, scaling group, NLB group, two protected original
IDs and the complete current source-hash set, including collector control,
lane-timing assessor, `sourceBootstrap`, `controller`, `client`, and `runner`.
The fixed lane map is exactly `pgcat_source_bootstrap.SOURCES`; all 20 modules
are staged together locally. The repeated three outer hashes must equal their
lane-map values. The outer `sourceHashes` has exactly
`controller`, `client`, and `runner`, each a lowercase SHA-256 of the current
local source bytes. All declared hashes are checked before either process starts.
The feed executable binding must be a non-symlink regular executable named
`aliyun` (resolve the Homebrew launcher to its actual Cellar executable); its
directory leads the minimal feed PATH and exact resolution is checked before
spawn. Scaling-group and NLB bindings must match the fixed provider targets.
Local `captureRoot` must not exist; `laneRoot`, provider status, lane lock,
intent directory, scale-out intent, NLB deregistration intent and proof paths are distinct direct children
of that new root. All worker paths are explicit fresh reviewed bindings;
local validation does not establish remote freshness or staging correctness.
Never reuse retired scratch paths, copy credentials into the manifest, or put
this private manifest in Git.

The reviewed worker layout has sibling `run` and `contract` directories under
one private mode-0700 run root. Before baseline launch, verify an existing empty
mode-0700 `run/disabled-boundary` directory. Both capture paths must still be
absent; do not reuse an old boundary or collector job. Missing boundary was a
confirmed pre-collector staging blocker on October 28; it is not safe to infer
that code import smoke checked this runtime prerequisite.
`workerInventory` is exactly `run/provider-inventory.json`;
the private provider source is `contract/provider-source`, outside the retained
capture tree. Each sequence writes an immutable ECS address sidecar there before
publishing the unchanged address-free inventory in the same remote write call.
The sidecar seals both the exact raw inventory bytes and the canonical parsed
inventory (`sort_keys=True`, compact JSON) so a selected cleaned collector frame
can be compared after the live inventory file has advanced.
The local success receipt follows both writes. A sidecar left by a failed local
receipt publication is unacknowledged and does not clear Pending proof. The
Pending consumer and retired producer still need their separate source review;
this local change supplies no live clearance.

The reviewed no-scale timing correction replaces the previous 75-second blocked
path with a **180-second maximum** feed loop at the unchanged 15-second interval.
Admission closes at **22:24 Shanghai**, reserving 120 seconds startup, 180 seconds
feed and 60 seconds cleanup before 22:30. No second feed or automatic extension
is permitted. This is a local implementation contract, not a live PASS receipt.

`start_no_scale()` starts one `LaneClient` in immutable `no-scale` mode before
one detached provider feed (`disabled-rehearsal`, 15-second interval, 180-second
maximum). The feed validates its exact-two target against sealed run context on
every publication. The fixed sequence is baseline start/status until PREPARED,
one finite **65-second collector** launch, exact ALIVE status, exact completed
EXITED status, then the source-pinned worker `lane-verdict`. The lane-timing
assessor requires at least 60 seconds of authenticated measurement within the
planned 65-second span, followed by an ordered terminal settling tail of at
most five seconds (total retained span at most 70 seconds). Every retained
frame remains subject to the three-second gap, identity and failure checks;
an authenticated SQL failure in the tail is FAIL. Clients, sources and the
protected original fleet must remain stable. Ordinary transition assessment
remains separate and cannot PASS from this capture alone.

Each worker call has a five-second absolute deadline from the shared protocol
clock, additionally bounded by `observedAt + 12 seconds`, and requires a strictly
later successful same-session desired-two publication. Existing receipt
freshness and one-use intent gates remain. Changed identity, missing evidence,
uncertain exit, failure to receive a successor or feed exit during a call stops
the run. No-scale admits no Pending, pre-drain, NLB, ESS, Gateway, client finish
or ConfigMap mutation.

No-scale feed launch uses `LaneClient.start_feed(argv, env, outer_binding_path)`.
The fixed reviewed command waits behind an inherited pipe before any provider
execution. The client retains the actual live `Popen` and seals the one-use
journal `feed_binding` (`runId`, fresh `feedId`, `pid`, original outer manifest
SHA-256 and argv SHA-256) before releasing one byte. EOF, startup timeout or a
failed binding prevents provider execution. Only the same retained handle with
an actual integer `poll()` result can seal `feed_exit_confirmation`. Direct
supervisor finish and the paid proof verifier require both independent records;
copies of caller-supplied PID, exit status or binding hash cannot substitute.
The paid feed launch path is unchanged.

After the verdict's successor, `stop_no_scale(verdict_result)` writes one private
fsynced stop intent, waits on its exact retained feed `Popen`, and checks the
matching acknowledgement and actual final receipt. At most one additional
publication is allowed during an in-progress tick. Natural timeout is not a PASS
path. The actual final receipt must be under 30 seconds old, and its sequence
is the final assessment's `endSequence`; no inventory timestamp is synthesized.
The fixed worker `lane-verdict` action seals its read-only assessment, then
removes only the bound worker `kubeconfig` under the contract's private parent,
fsyncs that directory, verifies absence, and seals a separate cleanup receipt.
The sanitized result contains status/category plus verdict, client-identity and
worker-credential-cleanup SHA-256 values. FAIL and INCONCLUSIVE can carry null
client/cleanup hashes when identity or cleanup is incomplete; the sealed SQL
FAIL and verdict digest remain intact even if unlink or fsync fails. PASS
requires valid hashes and verified cleanup. The required successor publication
uses the private inventory writer and does not need kubeconfig; any target drift
is refused before a ConfigMap update. A missing cleanup response is uncertainty,
not permission to retry the one-use action.

`finish_no_scale(feed_exit, assessment=None)` computes the final assessment from
the journaled worker result, exact stop intent/acknowledgement, actual final
receipt and feed exit. If an assessment is supplied, every field must equal the
derived result. It attempts exact local credential removal after feed exit on
all verdicts, preserves evidence, and never removes a replacement file. Cleanup
uncertainty prevents PASS. The private mode-0600 `lane/no-scale-proof.json`
records the Shanghai date, run/feed IDs, stable target/source/client hashes,
original binding-file hash, worker result, stop evidence and cleanup outcome.
Its exact canonical bytes determine `evidenceSha256`. The supervisor independently
compares the journaled worker result and current sealed final records before
committing finish. Retain the proof, journal and sealed finish receipts together.

A worker FAIL remains FAIL even when another check is incomplete. A `finished`
journal state never turns FAIL or INCONCLUSIVE into PASS. The client reconciles
a supervisor exit only against the exact durable journal and sealed final
receipts; process exit alone proves nothing about the assessment.

`close()` disconnects the supervisor on cancellation, retains the exact feed
handle and waits through the 180-second feed cap plus 60 seconds bounded grace
from feed launch. After confirmed exit it removes and verifies only the exact
unchanged local credential. Uncertain exit or cleanup writes a private mode-0600
`lane/cleanup-pending.json` with the bound path, PID and required operator action;
it never claims remote cancellation or signals a numeric PID. Controller death can leave the
already detached feed running to its existing bound. Preserve the global active
record and private evidence for exact feed/child/process/credential cleanup;
never clear them or start another run based only on terminal text. No-scale
proof cannot certify the later paid 3-to-2 reconciliation. Paid rehearsal and
full replacement remain blocked until their separate existing gates pass.

### Current release boundary and verified entry (2026-10-28)

This release structurally refuses `start_rehearsal`, paid `LaneClient.start`,
paid supervisor/feed entry, paid journal creation and admission from an existing
paid journal. There is no configuration enable flag. The paid component
contracts below remain offline development material. Re-enabling them requires
a separate reviewed implementation of the matching long boundary-driven capture,
Task 6D close/abort, and whole-path tests. A prior no-scale PASS cannot enable them.

Enter only from `python3 -I -S -c` with the following reviewed stdlib root.
Arguments are the absolute reviewed `pgcat_source_bootstrap.py` path, its
independently reviewed SHA-256, and the fresh private outer binding path. Do not
obtain the bootstrap by ordinary import, use `runpy`, or substitute `-B` (which
still reads caches). The root opens/hashes/compiles the bootstrap before any
application import; `load_controller` verifies and executes every reachable
application module from its opened source snapshot, preserves `__file__`, and
refuses preloaded/unlisted local modules. The finder stays installed for the
process lifetime. Runner, pipe-gated feed, and both Workbench wrapper roles enter
that same isolated boundary. The wrapper still execs the CLI in the same PID
with the inherited lock FD and original absolute deadline. Worker baseline,
Pending/pre-drain and zero-proof entries use the pinned phase dispatcher; direct
legacy proof scripts refuse execution. Collector/verdict retain their pinned entry.

The following is the complete `-c` program (the three paths/hash are arguments,
not interpolated code). This procedure is a release contract, **not current live
authorization**; all source review, staged-byte and action-day gates still apply.

```python
import hashlib, os, stat, sys, types
p, h, binding_path = sys.argv[1:4]
f = os.open(p, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
try:
    s = os.fstat(f)
    b = os.read(f, 262145)
    if not (stat.S_ISREG(s.st_mode) and s.st_uid == os.geteuid()
            and len(b) == s.st_size and len(b) <= 262144
            and hashlib.sha256(b).hexdigest() == h):
        raise ValueError('source')
finally:
    os.close(f)
m = types.ModuleType('pgcat_source_bootstrap')
m.__file__ = p
m.__verified_root_sha256__ = h
sys.modules[m.__name__] = m
exec(compile(b, p, 'exec', dont_inherit=True), m.__dict__)
controller = m.load_controller(binding_path)
try:
    controller.start_no_scale()
    while controller.call_no_scale('collector-status')['status'] != 'EXITED':
        pass
    verdict = controller.call_no_scale('lane-verdict')
    feed_exit = controller.stop_no_scale(verdict)
    print(controller.finish_no_scale(feed_exit))
finally:
    controller.close()
```

### Tracked guarded paid start (2026-10-27; offline component contract, disabled)

`Controller.start_rehearsal(preflight_path)` now refuses before preflight. The
following describes retained offline component logic, not a usable entry: independent source/binding review, separate
action-time authorization, a clean no-scale timing proof, staged recovery and
the earlier human/evidence gates remain mandatory. Keep Gateway
`replacementEnabled=false` and both historical scratch review flags false.
Task 5 stages nothing and provides no paid rehearsal PASS.

The argument names a fresh private mode-0600 `preflight.json` in a mode-0700
directory. The receipt has exactly these fields, rejects duplicate JSON keys,
and must be generated at action time from actual observations in Task 7:

- `target`: exactly the reviewed `laneBindings.target`.
- `checkedAt`, `expiresAt`: UTC ISO timestamps; age is nonnegative and under
  300 seconds, expiry is after the check and at most 300 seconds later.
- `gateway`: exactly `{replacementEnabled:false,idle:true}`.
- `clients`: exactly `{ready:2,sqlFailures:0,identitySha256:<SHA-256>}`.
- `metrics`: exactly `{fresh:true,complete:true}`; `singleOperator`: `true`.
- `noScaleProof`: exactly `{status:"PASS",evidenceSha256:<SHA-256>,proofPath:<absolute path>}`.
  Open the prior run's exact mode-0600, non-symlink `no-scale-proof.json` under its
  mode-0700 directory, outside the new paid capture root. Match its exact bytes,
  same-day date, stable target/source/client hashes and authenticated finished
  journal/receipts. The paid run uses a fresh binding-file hash, checked separately;
  its paths need not match the prior binding's provenance hash.
- `cost`: exactly `{day,net,reserve,allowance,ceiling}`. The day is today's
  Shanghai date; amounts are finite, nonnegative, ceiling is CNY 1,800 and
  `net + reserve + allowance <= ceiling`. Reuse the day's billing observation;
  this code does not query billing.
- `bindingSha256`: SHA-256 of the exact private binding file bytes.

Compute `clients.identitySha256` from current verified observations using the
same canonical JSON/SHA-256 formula as the worker: `{roster: sorted([[podUid,
podName,containerId,processId], ...]), deployment: {uid,generation,
observedGeneration,desired,ready}}`. Include exactly the two Ready clients.
A restarted process, changed Pod/container or changed Deployment invalidates the
prior proof. A guessed or manually chosen hash does not establish live identity.

This is an operator evidence receipt, not independent proof of live state.
Preserve its underlying evidence and authorization separately; never populate
it from guessed values, manual success flags or terminal output. It contains no
credentials, private addresses or raw provider responses. The start validates
it before any process, then re-reads it unchanged and unexpired immediately
before the proof, scale-out intent and request. Source hashes and fresh roots
remain enforced by the existing binding manifest.

Before starting the feed, the controller performs read-only STS account,
ESS/ECS/NLB snapshot, group desired/min/max and in-progress activity checks via
the exact bound Alibaba CLI. The exact originals must both be Protected/Healthy
and Available/Normal NLB backends of weight 100; desired/min are two, max four,
and no scaling activity may be in progress. The probe timestamp must remain
under five minutes old at intent and dispatch. Its environment preserves only
PATH/HOME/locale and the same allowlisted proxy keys as the provider feed.
Once the feed is live, the controller never makes a direct Alibaba or Workbench
call: worker actions and the single mutation go through `LaneClient`.

The fresh supervisor uses immutable `disabled-rehearsal` mode. The provider
feed keeps its 15-second publication interval. At actual feed spawn, its loop
cap is the smaller of 5,400 seconds and the remaining time to 22:30 Shanghai
minus a 120-second startup allowance; refuse unless allowance plus at least
75 seconds fits. The 21:30 paid cutoff is checked before the probe, after it,
at feed spawn, before intent and immediately before dispatch. **This scheduling
margin is not a guaranteed 22:30 child exit:** provider initialization precedes
its loop cap, and process startup/cleanup can overrun. Task 6/7 must own bounded
feed observation, stop and exact cleanup; do not infer cleanup from this cap.

The fixed sequence is baseline start/status to PREPARED, collector launch and
identity status, pre-scale start/status to PREPARED, then the same collector's
status again. Every worker call requires a strictly newer successful same-feed
publication observed after that call returned. Pre-scale proof must match the
feed and have a sequence from this pre-scale phase. Write exactly one private
`{feedSession,sequence}` proof and one exact 2-to-3 `scaleout.intent` using
O_EXCL plus file/directory fsync; neither can be overwritten. Intent binds
ModifyScalingGroup, account, group, desired two to three, feed session, current
sequence and UTC time. Its publication must be under 3.5 seconds old.

Submit `scaleout-two-to-three` once with an absolute 20-second deadline from the
shared protocol clock. Supervisor durable admission and shared one-use sequence
reservation remain authoritative. Only a complete journal acknowledgement plus
a strictly newer desired-three publication observed after ACK returns the
controller to `scaled-three`. Intermediate desired-two publications cannot
satisfy that gate. The ACK is not convergence evidence or rehearsal PASS.
Timeout, malformed/missing ACK, feed/session change, controller or supervisor
death, or any uncertain worker result leaves the run INCONCLUSIVE. Disconnect
the owner, retain the bounded feed handle and all private evidence; never retry
ESS, automatically reconcile/reverse, clear the global active-run record, or
claim cleanup. The paid continuation/close is the separate Task 6 boundary.

The local Task 6B NLB continuation binds one immutable Pending candidate in the
journal, then seals a separate worker pre-drain proof of two protected originals,
that sole unprotected candidate, unchanged clients and a healthy three-node
ESS/NLB roster. Its address-free summary is tied to the Pending proof and
reviewed worker source hash. The provider status receipt now includes
`rosterSha256`, computed from `{members,healthyRegistered,memberStates,backendStates}`
in the same successful publication. The NLB action requires its newest receipt
under 3.5 seconds old to equal the exact healthy three-node digest, plus a
same-session pre-drain proof under 30 seconds old. It reads a private, fsynced
one-use `nlb-deregister.intent` bound to that proof, candidate, group, session,
sequence and digest. The fixed local CLI request removes only the journal-bound
candidate as `Ecs` on port `6432`; it accepts only sanitized `RequestId` and
`JobId` within 20 seconds. A durable `(run, action)` key and the existing
sequence intent prevent retry under another request UUID. Unknown ACK stops;
there is no automatic retry or re-registration. Later same-session publications
may briefly retain the exact healthy three-node roster, then show only the
candidate as NLB `Removing` with its bounded weight/health states; once
`Removing` appears, a return to three Available backends refuses. A final
successful publication observed after ACK must show exactly the two healthy
protected originals in NLB, while the candidate remains the sole healthy
unprotected ESS member, before the controller records NLB absence. The ACK
alone is insufficient.

This is locally tested code, not live clearance. Paid start remains blocked
until the separate ESS 3-to-2, final close and abort stages have reviewed
implementation and the full action-day gates pass. ConfigMap `reconcile-close`
cannot substitute for either provider operation.

Receipt waiting also checks the retained supervisor process and active journal
through the client's existing identity-bound validator, including after a
matching receipt read and immediately before returning `scaled-three`. A
complete ESS acknowledgement cannot substitute for an active supervisor run;
death or cancellation observed at any of those boundaries is INCONCLUSIVE,
with no retry or automatic reversal. These checks are observations, not an
atomic guarantee against a later supervisor death or permission for the next
action, whose admission still belongs to the supervisor transaction.
