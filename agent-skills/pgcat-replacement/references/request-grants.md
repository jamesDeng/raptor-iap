# Request grant workflow

request-grants-contract: v1

Use this workflow when request_get returns request-grants-v1 in definition.operations[0].parameters.approvalProtocol. This workflow supersedes the legacy contracts and recovery procedure. Keep the same Request, pinned skill and resolved scope on every continuation. All infrastructure changes use the Infra MCP tools. Keep the conversation open for troubleshooting and use public progress to name the current phase or missing evidence.

## Read the request and request one grant

1. Call Raptor request_get for the bound Request. Read its operation prompt, scope digest and Infra approval policy.
2. Confirm the bound PgCat and application targets, and the complete human-approval policy from the Infra tool descriptions. Call Raptor request_grant_request in a separate round with requestId, envCode, scopeDigest and the capability bundle below. The runtime checkpoints and waits for the human decision. Collect live fleet and traffic evidence after approval, before the first write.

| Capability | Target | Constraints |
| --- | --- | --- |
| db-proxy.scale-out | db-proxy / bound proxy code | `{"desiredCapacity":4}` |
| db-proxy.node-protection | db-proxy / bound proxy code | `{"protected":true}` |
| db-proxy.drain | db-proxy / bound proxy code | `{"weight":0}` |
| db-proxy.sync-monitoring | db-proxy / bound proxy code | `{}` |
| application.restart | application / bound application code | `{}` |
| db-proxy.scale-in | db-proxy / bound proxy code | `{"desiredCapacity":2}` |

After each continuation, read request_get and then request_grant_get for the checkpointed grant ID in its own round. Wait for the result and use the exact verified approval. Re-read current observations before the next write. Use resource_wait with 60 seconds for a genuinely pending resource. Resume the same request and re-read its grant.

Use one tool call per write round. If a write returns EvidenceRefreshRequired, refresh the listed observations and wait for their results. That local rejection happens before submission. Retry only the same action with the same arguments and actionId. Reconcile an uncertain submission through the action receipt before any further write.

## Expand and qualify the replacement pair

1. After the grant is verified, discover the bound application with Raptor and Infra deployments_list and the two original nodes with cloud_read(fleet). Wait for those results. Keep the exact application workloadKey and original node IDs. If the request already saved an original pair, require those same IDs.
2. Call pgcat_connection_continuity with `{"phase":"hold"}` in its own round. Save its observationId. Gateway keeps one identified NLB TCP connection to each original and follows logs from both bound application pods. Require both original connections to execute SQL.
3. After hold returns, refresh the whole two-node baseline in one parallel read round using the workloadKey and node IDs from step 1. Include every item below and wait for all results.
   - Raptor request_get, environment_get, object_get and application deployments_list. Repeat request_get even though it was already used to read the grant.
   - Infra deployments_list for the proxy and deployment_status_get for the application with the discovered workloadKey. Include direct deployment_read.
   - cloud_read for fleet, backend and capacity, and db_connection_probe for each original ID.
   - metrics_read for trafficFailures, connections and pgcatClients. Put metrics at the end of the read batch so they remain fresh for expansion.
4. Require exactly two healthy, registered and protected originals, successful SQL on both, and the bound application 2/2 with healthy traffic and fresh metrics. If a discovery result disagrees with the earlier workloadKey or original IDs, stop for reconciliation. The evidence window is 30 seconds and PgCat source samples must be at most 15 seconds old. If the hold has aged out, repeat `pgcat_connection_continuity({"phase":"hold"})` alone. Require the same observationId and two socket IDs with successful SQL, then repeat the baseline batch.
5. In the next round, call Infra db_proxy_scale once with requestId, envCode, proxyCode, a new stable actionId and desiredCapacity 4. Save the returned ESS scaling activity ID. Use cloud_read with kind expansion to check that exact activity. If it is pending, wait and resume. A successful activity must list exactly two created instance IDs.
6. Verify the two provider-created IDs match the two new members alongside the fixed originals. Require healthy registration and successful direct db_connection_probe on each new node. Refresh the successful expansion activity read before calling pgcat_monitoring_sync with the bound requestId, envCode and proxyCode plus a new stable actionId. Re-read until all four nodes have fresh complete active, idle and waiting metrics. If a registered new target has yet to be scraped, wait and recheck. Once complete, protect both exact replacement IDs in one Infra call, then verify protection and health.

## Drain and restart

1. Refresh the direct SQL probes for the replacement pair and the four-node metrics. Set the NLB weight of both original IDs to zero in one db_proxy_nodes_deregister call. Leave the nodes attached and running. Save the returned actionId.
2. Confirm both originals still appear in the backend snapshot with zero routing weight and only the two replacements are eligible for new connections. Use the mandatory 60-second resource_wait, then re-read the grant. In fresh discovery rounds obtain both Raptor and Infra deployments_list and the dependent Infra deployment_status_get. Also obtain fresh deployment_read, capacity, fleet, backend and four-node metrics.
3. Call pgcat_connection_continuity with phase verify-drain, the saved observationId and drainActionId. Require SQL on the same two old sockets, and 24 new connections routed exclusively to the replacement pair with both represented. Any interrupted old socket or new route to an original fails acceptance.
4. Use phase release-probes with the same two IDs to close only the Gateway canary sockets. After release, do not call db_connection_probe. Then submit one Infra deployment_restart for the exact application workloadKey while the drain proof is fresh. Save its actionId and generation. Wait for that generation and 2/2 ready, updated and available replicas.

## Accept traffic and retire

1. Call pgcat_connection_continuity with phase verify-traffic, the observationId and restartActionId. The first sample may return traffic-warming. Wait and recheck. Require positive application query and transaction deltas on both new nodes, complete application logs without a failure, timeout, skipped operation or connection failure, and fresh raw active, idle and waiting counts of zero on each original across all pools and users.
2. Immediately before retirement, call metrics_read with kind pgcatClients and confirm both originals still have zero active, idle and waiting clients. Use the existing approved scale-in capability to submit one db_proxy_scale with a new stable actionId and desiredCapacity 2. Infra releases the two original IDs directly. Keep both originals protected until the release. Retain the ESS activity ID. If a write loses its response, use Raptor request_grant_actions_list to find the receipt by exact arguments, then refresh resource reads. Never submit an uncertain action again.
3. Check cloud_read retirement until ESS confirms successful release of exactly both originals. Confirm desired and fleet capacity two with only the healthy, protected replacement pair. Synchronize the final monitoring inventory and verify both targets are freshly scraped. Recheck application 2/2, then use phase verify-final-traffic with the same restart and observation IDs until both remaining nodes have positive post-retirement SQL deltas. Finish only after all these observations pass.
