---
name: pgcat-replacement
description: Use when reviewing or preparing replacement of PgCat ECS nodes managed by Aliyun ESS for one database proxy in a selected environment, or reconciling an interrupted replacement request.
---

# PgCat replacement

Status: draft reference; local fixtures and metric qualification exist. Live tool names, approval consumption, durable operation reconciliation, image bootstrap and full traffic evidence are not bound. Read [contracts](references/contracts.md) first. Until all enablement checks there are satisfied, perform read-only preflight and report missing contracts; do not call example/proposed commands or substitute cloud CLI/direct ECS deletion.

The skill chooses strategy. Infra API independently enforces membership, approvals and safety checks. A pinned skill never expands permission. Record request/attempt IDs, proxy/environment/group/DB identities, original node IDs, desired capacity, protection states, app Deployment UIDs and exact selected skill commit. Re-discover all associations using catalog codes/tags; a proxy serves one same-environment DB, a DB can have several proxies.

## Initial two-node replacement reference

The following is a strategy for the dedicated test stack, not a built-in end-to-end API operation. Resolve actual reviewed tools through the contracts document.

1. Inspect current provider inventory and application/metric baseline. Require complete current nodes/pools, valid observations, known operation states and an actual running correctness workload. Protect original nodes. Scale 2→4 and wait for two distinct new healthy registered nodes, successful SQL readiness, qualified client metrics and traffic evidence. Protect retained new nodes as well.
2. Select one original node. At desired 4, deregister only that node: three healthy registered nodes remain, and 3 > 4/2. Two at once would leave equality and must be refused. Deregistration needs no human approval, but Infra API checks the strict majority gate afresh.
3. Restart only discovered associated application Deployments through the reviewed restart command, preserving target UID checks; await deployment status and traffic evidence. Wait for selected old node's connected clients (idle + active + waiting) to reach zero. Backend idle connections are irrelevant. Unprotect only the selected old node; node protection needs no approval but validates membership.
4. Ask for a fresh approval bound to this request, logical operation, environment, proxy/group and exact target capacity 3. Scale 4→3. Infra API must check valid zero connected clients on EVERY unprotected node, not an assumed ESS selection. Confirm the old node was removed and retained nodes are healthy/protected. A timeout/partial/unknown result pauses mutations for reconciliation.
5. Repeat for the other original node at desired 3: deregistration leaves two healthy nodes and 2 > 3/2. Restart/reobserve, unprotect that old node, obtain a distinct new approval for 3→2 and confirm removal. Record final node identities and restore recorded protection policy on retained nodes after replacement completes.

## Measurements and pauses

Raw metric mapping is `pgcat_pools_cl_active + pgcat_pools_cl_idle + pgcat_pools_cl_waiting`, summed across every expected static pool/user. Missing/duplicate series are not zero. Verify up=1, source observation age at most the reviewed threshold (proposed 15 seconds), full node/pool coverage and unchanged config revision. A fresh recording-rule timestamp does not freshen an old scrape. Pool reload/removal is outside current qualification.

The owner accepted new connections arriving between zero-client observation and termination. Do not add mandatory deregistration/routing exclusion to the scale-in gate. Use deregistration as strategy and measure outcomes; never guarantee zero failures. Report scheduled/attempted/successful/failed/timeout/ambiguous/skipped work, counter resets, coverage and connection errors over baseline/replacement/recovery windows. Missing traffic is inconclusive, not success.

Resume from [recovery](references/recovery.md), with fresh provider inventory and durable operation status. A retry retains logical operation identity and must reconcile a prior outcome; a new reduction always needs a new approval. Do not replay mutation steps from conversation text.
