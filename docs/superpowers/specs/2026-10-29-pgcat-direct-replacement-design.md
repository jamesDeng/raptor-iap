# PgCat direct 2→4→2 replacement

## Owner-selected behavior

The owner explicitly directs: expand ESS 2→4; qualify both new nodes; stop new
connections to both original nodes; rolling-restart the related application
Deployment; observe fresh zero active, idle and waiting client connections across
every pool/user on both originals; then scale ESS directly 4→2. Retain exactly
the two replacements. The staged desired-three action and a new session-rotation
command are superseded. This is authorized implementation, test and review work;
repeat design approval is not required.

## Safety and authority

Keep the generic strict-majority deregistration rule. Add a narrow exception for
the complete four-member, desired-four/min-two/max-four proxy: removal may leave
exactly two registered healthy protected members. It must never admit one
remaining member, unhealthy/unprotected retained capacity, foreign or duplicate
IDs, or an incomplete/changed provider snapshot. Existing exact scope, fleet
serialization and fresh unchanged-snapshot checks remain mandatory.

For direct 4→2 scale-in, require exactly two protected healthy registered retained
members and exactly two unprotected withdrawn members; the existing all-pool
fresh-zero metrics, exact approval, durable claim and no-replay boundaries remain.
Capacity safety is enforced by Infra independently of model reasoning. Infra's
generic snapshot does not establish original/new lineage: the harness must bind
all replacement mutations to its persisted request baseline and the actual
ready-four replacement cohort. No new model-submitted baseline or caller-chosen
capacity exemption is trusted.

Require both exact originals withdrawn and both replacements protected before
restarting the scope-bound application. Require the observed application rollout
and fresh complete zero metrics for both originals before unprotecting/scaling.
On resume, obtain fresh evidence; a historical ready-four witness establishes the
replacement cohort, not current readiness. Reject a desired-three mutation and
arbitrary replacement IDs on the direct path. Unknown mutations are reconciled
from actual observations and never blindly replayed.

## Contract and historical compatibility

Use replacement convergence version 2 for the two exact action stages 2→4 and
4→2, unique action IDs, two distinct originals and the same two replacements
identified at ready-four. Acceptance remains pending until retained evidence and
cleanup are independently assessed. Keep version 1 staged stored results readable;
never reinterpret three staged actions as a version-2 success.

Version2 also exports `restartGeneration`, a positive safe integer taken from the
actual request-bound Deployment restart receipt. The external native observer can
then bind the real rollout to the authenticated Gateway result; generic progress
events do not carry that generation. Version1 does not acquire a fabricated
restart generation. Include every new harness module in both the Gateway image
and fixed sandbox staging manifest.

Publish updated skill instructions in a new immutable release (candidate
skills-v0.5.0); never move skills-v0.4.0. Bind execution to verified selected
skill content and a fresh immutable request, without converting an old request.
The harness must reject an incompatible selected skill before any mutation.

Add an explicit provider/capture mode for the direct sequence. Preserve absent or
full-replacement mode as historical staged behavior and disabled-rehearsal mode
as the existing 2→3→2 rehearsal. A direct desired drop 4→2 permits the two exact
original departures, including staggered provider membership convergence under
the one action; it never permits a third identity/departure, an original retained
at completion, or a replacement leaving. Bind one feed session and capture;
preserve complete metrics until each old node has its own zero and subsequent
backend-absence evidence. Failure precedence and freshness/gap limits do not change.

## Application restart and evidence

Use the existing RollingUpdate strategy, maxUnavailable 0 and maxSurge 1. Verify
SIGTERM stops scheduling while draining already accepted SQL work under finite
deadlines and closes active and idle sessions. Retain final counters for retiring
Pods and all startup/counter events for successor Pods in the same capture. A
rollout does not reset adverse counters or prove old connections zero. Check the
termination grace period covers drain, final-evidence hold and server shutdown;
do not forcibly close a running transaction to manufacture zero.

## Release and proof

Use test-first changes, task review and a whole-graph review. Keep replacement
disabled and existing public paid admission structurally closed throughout code
preparation. Implement native observer closure using the reusable owner/abort,
ordinary verdict, restoration and credential gates; do not finish candidate-only
original-roster rehearsal stages and then duplicate them for restarting clients.
Freeze one source graph, run the disabled no-scale restart/evidence/closure proof,
and only then execute the new full request through the existing reviewed GitOps
release and runtime flow. The superseded paid 2→3→2 candidate rehearsal is not a
prerequisite for this new strategy; its historical verdict remains unchanged.
No live PASS follows from source tests. Any uncertain state stops new mutations.

Use the existing project/checkout; preserve unrelated iCloud files. No new cloud
resources or IAM are required by this design. Terraform/GitOps remains the
provisioning owner. Keep credentials private and use Workbench CLI first for
authorized worker operations. Budget ceiling CNY 1,800 net, one billing check per
day only at live preflight, paid-action cutoff 21:30 and POC stop 22:30 Shanghai.
