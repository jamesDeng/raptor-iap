---
name: pgcat-replacement
description: Use when replacing or reconciling PgCat ECS nodes in the dedicated two-node Aliyun ESS test proxy under an immutable Raptor request.
---

# PgCat replacement

replacement-contract: direct-v2

Release candidate: skills-v0.5.0. Publish as a new immutable release; never move skills-v0.4.0. The runtime selects this contract from verified content, not a tag name.

Use the [runtime contracts](references/contracts.md) and [recovery rules](references/recovery.md). These instructions match the implementation under qualification; they do not establish live deployment or authorize enablement. Begin only after controller preflight enables this dedicated request.

Keep the same request ID, definition, resolved scope and selected skill SHA across every resume. A fresh attempt is not a new request. Use only the supplied group, backend, database and application binding; never derive application dependencies from unrelated tags. Infra API independently enforces membership, exact approval and safety gates.

## Direct two-node strategy

1. Read request/environment/object, provider identity and proxy deployment. Read the bound application Deployment and cloud fleet, capacity and backend plus trafficFailures, connections and pgcatClients metrics. Require exact original desired/fleet two, healthy registration, complete fresh raw pool/user triplets, running correctness traffic and successful read-only SQL probes on both originals. Protect both originals and observe protection before expanding 2→4 once with a stable unique action ID.
2. Observe all four fleet/backend members, desired four, complete fresh metrics and SQL success on the exact two new nodes. During boot use resource_wait(seconds=60). A persisted readyFour witness defines the only replacement pair. Protect that pair and freshly observe both protected and healthy. Never replace the cohort with arbitrary IDs.
3. At desired four, deregister both exact originals in one batch or two disjoint batches. Observe both absent from the backend while both replacements remain protected, healthy and registered. Only then rolling-restart the scope-bound client Deployment once.
4. Read deployment_read until its actual observedGeneration reaches the restart receipt generation and replicas, updatedReplicas, readyReplicas and availableReplicas are all two. During the surge use resource_wait(seconds=60) and stop the completed round. Traffic client discovery requires exactly two Ready clients: do not query trafficFailures/connections during the surge or treat discovery refusal as successful traffic. After rollout, require fresh complete PgCat metrics with active, idle and waiting zero for BOTH originals across every pool/user. Missing series are not zero.
5. Unprotect only both originals, observe that change and recheck retained protection/health, both backend absences, bound rollout generation and fresh complete zero metrics. Request one exact 4→2 approval with a distinct stable action ID, then immediately request_pause(reason=waiting_approval). Stop the completed tool round. On verified resume repeat current evidence and approval/control checks. Submit the exact 4→2 action once. Never mutate desired three.
6. Reconcile the single reduction until desired/fleet/backend are exactly the two readyFour replacements, healthy, registered and protected, both originals absent and the bound application rollout ready. Return convergence version 2 with only the 2→4 and 4→2 actions; acceptance remains pending.

The owner accepts connections arriving between zero-client observation and termination. Do not promise zero failures or add new mandatory scale-in conditions. Full retained traffic and approval/gate evidence determine PASS, FAIL or INCONCLUSIVE independently; fleet convergence alone is not acceptance.
