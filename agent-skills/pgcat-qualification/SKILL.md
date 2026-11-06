---
name: pgcat-qualification
description: Qualify the scoped PgCat observation and approval-continuation runtime without changing infrastructure.
---

# Read-only native qualification

Use only when the trusted request binding is `db-proxy.replace-nodes` with `executionMode: qualification`. This is not a replacement strategy or infrastructure permission. Never scale, deregister, change protection or restart anything. Those commands are denied server-side, including after an approval. Never seek CLI, Kubernetes or direct cloud write workarounds. A different mode requires a new request, not editing this session.

Read the bound request, environment and proxy using `request_get`, `environment_get` and `object_get` through Raptor MCP. Infra verifies cloud identity internally. Discover the exact proxy by code and read the associated application's exact Deployment status through Infra MCP. Call Infra deployment_status_get for the trusted application using the runtime read plan’s exact arguments. The runtime can resolve an omitted workloadKey only after unique scoped discovery. Also call direct deployment_read({}); each read produces its own required receipt. Check the returned identities against the trusted scope. Missing, ambiguous, changed or stale evidence stops qualification.

Use the four scoped observation tools:
- `cloud_read`: fleet, backend and capacity. Require a stable baseline of exactly two or four healthy ESS members, desired capacity matching that count, and every member registered and healthy in the bound backend. A two-node baseline must be protected. For four nodes, record protection as observed; this read-only test does not change it. Persist all member IDs and require the identical set after every continuation.
- `deployment_read`: the exact associated client Deployment. Require matching cluster/namespace/name/UID and a ready rollout.
- `metrics_read`: trafficFailures, connections and pgcatClients. These read fresh, complete Prometheus series. PgCat connected client counts include active, idle and waiting clients; count clients from metrics, never infer them from a SQL probe.
- `db_connection_probe`: fixed SELECT1 through each discovered healthy PgCat node. This checks database connectivity only; it does not count clients or write data.

On the first segment, complete every read and probe every baseline node. Then call `resource_wait` with `{seconds: 60}` and stop at that completed tool round. This exercises automatic checkpoint/resume without cloud changes.

After verified resource continuation (no approval ID), repeat every read and every node probe for the saved baseline. Then use Raptor `approval_request` with a fresh action ID, interface `runtime.qualification-resume`, the exact environment, target `{proxyCode}` from the trusted scope, and parameters `{phase: "resume"}`. Then immediately call `request_pause` with reason `waiting_approval` and finish that tool round. State clearly that this approval resumes a read-only test and authorizes no infrastructure change. Do not poll in a model loop or make more calls after the pause.

After the Gateway supplies verified continuation context for this exact approved action, read `approval_get` and verify it belongs to this request. Repeat every read check in this new active segment; never reuse old observations as current state. Every member ID must match the saved baseline. Approval does not change the read-only boundary. If denied, blocked or cancelled, obey the request control and guidance; do not claim qualification complete.

Follow the runtime read batches in separate assistant responses: wait for each batch’s successful results before generating the next. Discovery and calls using its returned selectors belong in different responses, even with sequential execution. Never fill an unknown selector with a placeholder. Batch only independent reads with already-known arguments without generating dependent calls before their results. Source validators enforce scrape and endpoint freshness; if observations are incomplete, expired or inconsistent, stop rather than claiming success.

Finish with a short read-only observation summary. The harness derives the qualification report from tool receipts and verified continuation. No assistant statement can establish replacement convergence, zero failed operations across a traffic window, or completion of the overall POC. A new replacement requires its own request; recovery of an existing replacement keeps its original request and saved cohort. All real scale-ins require their exact approvals.
