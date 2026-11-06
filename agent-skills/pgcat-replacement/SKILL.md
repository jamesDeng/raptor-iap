---
name: pgcat-replacement
description: Use when replacing or reconciling PgCat ECS nodes in the dedicated two-node Aliyun ESS test proxy under an immutable Raptor request.
---

# PgCat replacement

replacement-contract: resource-v1

Release candidate: skills-v0.5.5. Publish a new immutable release; preserve all earlier tags. The runtime selects this contract from verified content, not a tag name.

Use the [runtime contracts](references/contracts.md) and [recovery rules](references/recovery.md). These instructions match the implementation under qualification; they do not establish live deployment or authorize enablement. Begin only after controller preflight enables this dedicated request.

Keep the same request ID, definition, resolved scope and selected skill SHA across every resume. A fresh attempt is not a new request. Use only the supplied group, backend, database and application binding; never derive application dependencies from unrelated tags. Infra API independently enforces membership, exact approval and safety gates.

## Initial read rounds

Use two assistant responses. Sequential execution does not supply results while generating the same response.

1. **Discovery round:** call scoped Raptor application deployments_list and cloud_read(fleet). End the response; wait for both successful results. Do not include deployment_status_get, SQL probes or mutations in this round.
2. **Evidence round:** after receiving results, copy the exact workloadKey and original IDs into dependent calls. Never generate placeholders such as "unknown". Missing/ambiguous selectors require discovery or a stop. Required receipts:
   - Raptor request_get, environment_get, object_get and application deployments_list.
   - Infra proxy deployments_list and application deployment_status_get({envCode, appCode, workloadKey}).
   - Direct deployment_read({}); cloud_read fleet/backend/capacity; db_connection_probe for both discovered originals; metrics_read trafficFailures/connections/pgcatClients last.

Both Deployment reads are required. Batch independent reads with already-known arguments together; the runtime can run an all-read batch concurrently. Never mix a mutation or resource_wait into that evidence batch. Wait for the complete evidence round before mutations. Refresh aged receipts: runtime freshness is 30 seconds, PgCat metric sources 15 seconds. After resume, repeat discovery and wait before generating dependent calls.

After SQL succeeds, inspect the entire checklist. If only read tools remain available, complete or refresh missing/aged slots and continue reading; the baseline is not finished. Only a completed valid baseline unlocks the first protection or expansion call. A missing write tool means required evidence is still missing, stale or invalid; refresh the needed reads. Do not call resource_wait to wait for an expansion that has not been invoked.

## Direct two-node strategy

1. Complete the initial read rounds above, including both Infra deployment_status_get and direct deployment_read. Require exact original desired/fleet two, healthy registration, complete fresh raw pool/user triplets, running correctness traffic and successful read-only SQL probes on both originals. If both originals are already protected, retain that fresh proof without a redundant write. Otherwise protect both and observe protection before expanding 2→4 once with a stable unique action ID.
2. Observe all four fleet/backend members, desired four and complete fresh PgCat client metrics. Both new nodes must be healthy and registered. Require complete metrics; zero clients is valid before restart. Do not require SQL on the new pair. The runtime checkpoints resource_wait(seconds=60) in the completed expansion round, even when expansion uses turn ten. Stop there. On resume rediscover scoped workloads and inspect current state; during boot use resource_wait(seconds=60). A persisted readyFour witness defines the only replacement pair, not a substitute for fresh health and metrics on resume. Protect that pair and freshly observe both protected and healthy. Never replace the cohort with arbitrary IDs.
3. At desired four, deregister the exact originals in one or two batches only after verifying NLB withdrawal preserves established sessions. Keep both nodes running and their sessions intact. Observe both absent from the backend while both replacements remain protected, healthy and registered. Perform a mandatory 60-second resource_wait and stop. On verified resume, rediscover the opaque application workloadKey and collect fresh fleet, backend, capacity, complete metrics, and bound Deployment evidence. Only after the persisted wait completes and those checks succeed, rolling-restart the scope-bound client Deployment once.
4. Read deployment_read until its actual observedGeneration reaches the restart receipt generation and replicas, updatedReplicas, readyReplicas and availableReplicas are all two. During the surge use resource_wait(seconds=60) and stop the completed round. Traffic client discovery requires exactly two Ready clients: do not query trafficFailures/connections during the surge or treat discovery refusal as successful traffic. After rollout, require at least one active or idle client on each replacement. Require fresh complete PgCat metrics whose TOTAL active+idle+waiting across BOTH originals and every pool/user is a combined integer 0 or 1; refuse 2 or more. This is one combined budget, never one per node. Exclude protected retained-node clients from that retiring budget. Missing series are not zero.
5. Unprotect only both originals, observe that change and recheck retained protection/health, both backend absences, bound rollout generation and fresh complete metrics within the same combined retiring-node budget of one. Require at least one active or idle client on each exact new node. Request one exact 4→2 approval with a distinct stable action ID, then immediately request_pause(reason=waiting_approval). Stop the completed tool round. On verified resume rediscover the application workloadKey and repeat current health, complete metrics including each new node's active or idle client, and approval/control checks. Submit the exact 4→2 action once. Never mutate desired three.
6. Reconcile the single reduction until desired/fleet/backend are exactly the two readyFour replacements, healthy, registered and protected, both originals absent and the bound application rollout ready. Return convergence version 2 with only the 2→4 and 4→2 actions; acceptance remains pending.

The owner accepts exposure of up to one observed retiring-node connection plus connections arriving between observation and termination. This does not establish zero loss. Do not promise zero failures or add new mandatory scale-in conditions. Full retained traffic and approval/gate evidence determine PASS, FAIL or INCONCLUSIVE independently; fleet convergence alone is not acceptance.
