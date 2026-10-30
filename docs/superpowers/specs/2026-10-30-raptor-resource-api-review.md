# Infra API resource boundary review — 2026-10-30

Status: verified source audit and owner requirement; correction not implemented or deployed.
Source inspected: 42375f7609660e06434f02f3ccc46efbc6ba41bb (Infra source from main b3bf0e4).

## Owner correction

Agent obtains Raptor resource codes through Raptor MCP. Infra API inputs identify Raptor resources by environment and code, not arbitrary public-cloud resources. Infra resolves the backing provider resources internally. Responses describe Raptor workloads, e.g. pgproxyWorkload with type ess, rather than exposing an ESS operation as the product API. A separate scope.json mapping is not the desired resource source of truth.

## Verified inventory

All currently registered Infra MCP operations were inspected through internal/api/mcp.go and commands.go; HTTP dispatch inspected through server.go and operations.go.

| Operation | Current boundary | Required correction |
| --- | --- | --- |
| cloud_identity_get | envCode; cloud account check | Internal provider validation; do not make cloud identity the agent-facing business operation. |
| deployments_list | envCode/kind/code | Already logical input; normalize provider-shaped deployment output into Raptor workload identity. |
| deployment_status_get | appCode plus clusterId/namespace/name/uid | Resolve workload from envCode/appCode and validate current identity internally. |
| proxy_observation_get | proxyCode plus groupId/serverGroupId; fleet/backend/capacity DTOs | Resource-only selector; Raptor pgproxyWorkload response with provider implementation details nested. |
| proxy_pgcat_metrics_get | proxyCode plus groupId/serverGroupId; raw Prometheus rows | Resource-only selector; metrics bound to the resolved proxy and its workload nodes. Preserve timestamp/completeness semantics. |
| deployment_restart | appCode plus clusterId/namespace/name/uid | Resolve application internally; preserve exact UID, request binding, approvals and concurrency protections. |
| db_proxy_scale | proxyCode plus groupId | Resolve proxy workload internally; retain minimum/desired/approval/connection guards. |
| db_proxy_node_protection_set | proxyCode/groupId/instanceIds | Proxy-scoped workload node identity; provider IDs validated and resolved internally. |
| db_proxy_nodes_deregister | proxyCode/groupId/serverGroupId/instanceIds | Proxy-scoped workload node identity; provider target resolution internal. |

The cloud identity, discovery and deployment status reads also have HTTP routes. Four commands have HTTP and MCP dispatch. New proxy observations are MCP-only. Changing transport alone does not correct the resource boundary.

## Existing foundations and mismatch

- Raptor MCP already contains request_get, environment_get, object_get, deployments_list. Scoped agent variants must be checked, not assumed identical to the general service catalog.
- Raptor environment catalog stores cloud, region, account and cluster configuration (internal/catalog/environments.go).
- Infra discovery already filters cloud tags by env and db-proxy-code/db-code (internal/provider/aliyun.go).
- Infra currently loads account/cluster/proxy mapping from scope.Load and compares submitted provider IDs to that static mapping. This is duplication, not a necessary consequence of MCP.
- Existing Raptor replacement authorization also carries provider IDs; hiding input fields alone is insufficient. Authorization and provider resolution must move together while retaining request isolation.

## Proposed correction boundary

Use existing Raptor catalog/environment data as authority. Authenticate the request and authorize env/resource code before provider access. Infra resolves current provider mappings from authoritative configuration and verified tags, rejecting missing, ambiguous or foreign mappings. No caller-supplied cloud IDs may override resolution. Keep provider adapters internally; do not add AWS support as part of this Alibaba POC.

Illustrative tool contract (names are proposal, not implemented): pgproxy_workload_get({envCode, pgproxyCode}), with request binding supplied by authenticated context. Return {envCode, pgproxyCode, pgproxyWorkload:{type:"ess", desiredSize, minimumSize, nodes, ...}, observedAt}. Load-balancer and metric data remain associated with this logical resource. Provider type is allowed; provider objects are implementation details, not independent query targets.

Resolve exact target identities server-side for mutations and bind approval to that resolved identity/revision. Preserve minimum-change and desired-reduction approvals, desired>=minimum, retained healthy capacity, withdrawal before app restart, fresh complete combined active+idle+waiting<=1 across retiring pair, no replay, and fail-closed missing metrics.

## Release disposition

Hold PR305 and existing Infra function deployment. The previous plan to retrieve scope.json is superseded by this architecture correction; no archive download is needed for this review. Do not deploy an empty/arbitrary replacement scope as a shortcut. No cloud mutations performed for this review. Full replacement remains NOT PASS.

Next: trace catalog-backed resolver ownership and scoped Raptor MCP availability, then produce a bounded implementation plan and migration inventory. A broad rewrite, new controller/service, or new cloud provider requires owner approval before implementation. This review does not claim the correction is complete.
