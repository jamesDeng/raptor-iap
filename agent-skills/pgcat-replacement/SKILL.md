---
name: pgcat-replacement
description: Use when replacing or reconciling PgCat ECS nodes in the dedicated two-node Aliyun ESS test proxy under an immutable Raptor request.
---

# PgCat replacement

Use the [runtime contracts](references/contracts.md) and [recovery rules](references/recovery.md). These instructions match the implementation under qualification; they do not establish live deployment or authorize enablement. Begin only after controller preflight enables this dedicated request.

Keep the same request ID, definition, resolved scope and selected skill SHA across every resume. A fresh attempt is not a new request. Use only the supplied group, backend, database and application binding; never derive application dependencies from unrelated tags. Infra API independently enforces membership, exact approval and safety gates.

## Two-node strategy

1. Read request/environment/object, provider identity and proxy deployment. Read `cloud_read` fleet, capacity and backend plus `metrics_read` trafficFailures, connections and pgcatClients. Require original desired/fleet two, healthy registration, complete fresh pool triplets and running correctness traffic. Record both original IDs. Protect originals, then expand 2→4 once with a stable unique action ID.
2. Observe readiness; use `resource_wait` with seconds 60 for boot/drain/rollout waits. Never resubmit an acknowledged or unknown action. Qualify both new nodes through healthy registered backend and SQL probes; protect both new nodes.
3. At desired four, deregister only one original: three healthy registered nodes remain, strictly greater than 4/2. Restart only the associated client Deployment. Observe its readiness and traffic, then require that old node's active + idle + waiting clients across every reported pool/user equal zero. Missing series are not zero. Unprotect only that original; all other nodes remain protected.
4. Request exact 4→3 approval, then immediately call `request_pause` with reason `waiting_approval`. Stop the completed tool round. After verified resume, check fresh approval/control and observations, then submit that exact action once. Observe removal of the selected old node and retained health.
5. Repeat for the other original at desired three: deregistration leaves two healthy registered nodes, strictly greater than 3/2. Drain and unprotect only it. A distinct action ID and a new exact 3→2 approval are required. Finish at two new healthy, registered, protected nodes with both originals absent and associated client ready.

The owner accepts connections arriving between zero-client observation and termination. Do not promise zero failures or add new mandatory scale-in conditions. Full retained traffic and approval/gate evidence determine PASS, FAIL or INCONCLUSIVE independently; fleet convergence alone is not acceptance.
