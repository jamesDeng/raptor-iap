---
name: pgcat-qualification
description: Qualify the scoped PgCat observation and approval-continuation runtime without changing infrastructure.
---

# Read-only native qualification

Use only when the trusted request binding is `db-proxy.replace-nodes` with `executionMode: qualification`. This is not a replacement strategy or infrastructure permission. Never scale, deregister, change protection or restart anything. Those commands are denied server-side, including after an approval. Never seek CLI, Kubernetes or direct cloud write workarounds. A different mode requires a new request, not editing this session.

Read the bound request, environment and proxy using `request_get`, `environment_get` and `object_get` through Raptor MCP. Check cloud identity, discover the exact proxy by code and read the associated application's exact Deployment status through Infra MCP. Check the returned identities against the trusted scope. Missing, ambiguous, changed or stale evidence stops qualification.

Use the four scoped observation tools:
- `cloud_read`: fleet, backend and capacity. Require exactly the same two healthy protected ESS members, desired capacity two, and both members registered and healthy in the bound backend.
- `deployment_read`: the exact associated client Deployment. Require matching cluster/namespace/name/UID and a ready rollout.
- `metrics_read`: trafficFailures, connections and pgcatClients. These read fresh, complete Prometheus series. PgCat connected client counts include active, idle and waiting clients; count clients from metrics, never infer them from a SQL probe.
- `db_connection_probe`: fixed SELECT1 through each discovered healthy PgCat node. This checks database connectivity only; it does not count clients or write data.

On the first segment, complete the above checks, then use Raptor `approval_request` with a fresh action ID, interface `runtime.qualification-resume`, the exact environment, target `{proxyCode, groupId}` from the trusted scope, and parameters `{phase: "resume"}`. Then immediately call `request_pause` with reason `waiting_approval` and finish that tool round. State clearly that this approval resumes a read-only test and authorizes no infrastructure change. Do not poll in a model loop or make more calls after the pause.

After the Gateway supplies verified continuation context for this exact approved action, read `approval_get` and verify it belongs to this request. Repeat every read check in this new active segment; never reuse old observations as current state. The two member IDs must match the saved baseline. Approval does not change the read-only boundary. If denied, blocked or cancelled, obey the request control and guidance; do not claim qualification complete.

Batch independent reads where supported to stay within the active segment's ten-turn / ninety-second limit. Source validators enforce scrape and endpoint freshness; if observations are incomplete, expired or inconsistent, stop rather than claiming success.

Finish with a short read-only observation summary. The harness derives the qualification report from tool receipts and verified continuation. No assistant statement can establish replacement convergence, zero failed operations across a traffic window, or completion of the overall POC. A subsequent real replacement uses a new request and separately approved scale-ins.
