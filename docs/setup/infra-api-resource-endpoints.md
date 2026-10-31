# Current Infra resource endpoints

Source inventory, 2026-10-31. This describes `cmd/server/main.go` → `buildResourceRuntimeHandler` → `api.NewResourceServer`, not a deployed receipt. Caller auth uses the configured header (active templates: `X-Infra-Authorization`); responses use safe error codes and `Cache-Control: no-store`. Service Basic credentials and opaque attempt credentials have distinct authorization paths. Unknown, duplicate and extra selectors are refused.

| HTTP entry | Public input | Exposure |
|---|---|---|
| `GET /healthz` | none | Unauthenticated readiness; not a Gateway route |
| `GET /v1/deployments` | `envCode`, `kind`, `code` | `kind`: application/database/db-proxy; typed resource/workload envelope |
| `GET /v1/deployment-status` | `envCode`, `appCode`, optional `workloadKey` | Omitted key requires one workload |
| `GET /v1/deployments` with exact `Accept: application/vnd.raptor.resolution-proof+json` | `envCode`, `kind=db-proxy`, `code` | Service-only private proof on the existing routed path; agent denied before introspection/resolution |
| `GET /v1/resource-resolution-proof` | `envCode`, `proxyCode` | Local/internal service-only equivalent; not routed by Gateway |
| `POST /v1/deployment-restart` | `requestId`, `envCode`, `appCode`, `workloadKey` | Restart opt-in |
| `POST /v1/db-proxy-scale` | `requestId`, `envCode`, `proxyCode`, `actionId`, `desiredCapacity` | Proxy-command opt-in; existing exact approval/fleet/capacity guards |
| `POST /v1/db-proxy-node-protection` | `requestId`, `envCode`, `proxyCode`, `nodeIds`, `protected` | Proxy-command opt-in; current proxy membership |
| `POST /v1/db-proxy-node-deregistration` | `requestId`, `envCode`, `proxyCode`, `nodeIds` | Proxy-command opt-in; backend resolved privately |
| `/mcp` | No URL query; typed MCP payload | Stateless Streamable HTTP; Gateway exposes POST |

Current MCP tools:

| Tool | Required arguments (optional explicitly marked) |
|---|---|
| `deployments_list` | `envCode`, `kind`, `code` |
| `deployment_status_get` | `envCode`, `appCode`; optional `workloadKey` |
| `proxy_observation_get` | `requestId`, `envCode`, `proxyCode`, `kind`: fleet/backend/capacity |
| `proxy_pgcat_metrics_get` | `requestId`, `envCode`, `proxyCode`, `series`: up/upTimestamp/active/activeTimestamp/idle/idleTimestamp/waiting/waitingTimestamp |
| `deployment_restart` | Same fields as HTTP restart |
| `db_proxy_scale` | Same fields as HTTP scale |
| `db_proxy_node_protection_set` | Same fields as HTTP protection |
| `db_proxy_nodes_deregister` | Same fields as HTTP deregistration |

All four reads are registered. Optional command discovery also requires configured commander/authorizer and allowed paths. Question/qualification credentials receive no mutation tools; operation authorization independently refuses qualification mutations. Proxy reads require an exact replacement binding (including qualification execution mode) and request/environment/proxy. Related app/database access is pinned; application questions stay primary-only.

No active MCP tool or HTTP operation supplies cloud identity. The retained Terraform GET `/v1/cloud/identity` route reaches `NotFound` in the active resource handler; the legacy service-authorizer allowlist is not an implementation. Account checks occur inside provider resolution. No cloud-selector diagnostic bypass is active. Legacy `buildHandler`, `buildHandlerWithAgent`, `buildRuntimeHandler`, `NewWithAgentCommands` and the old `mcp.go` catalog survive only as unreachable production constructors/test surfaces; `main` has no fallback to them. No route is added or removed by this inventory.

Model selectors contain no `groupId`, `serverGroupId`, `listenerId`, cluster/namespace/name/UID, arbitrary URL or PromQL. Opaque `workloadKey` does not confer provider authority. Observed `nodeIds` are checked against the chosen proxy's current membership. Returned workload identities and nested fleet/backend/metrics can contain provider IDs as evidence. Private proof never becomes an MCP tool. Internal reuse of `operations.proxyRead` happens only after logical authorization/resolution with a request-local environment and private resolved targets.

Infra's fixed catalog request is `/v1/infra/resource-context?envCode&kind&code`; Raptor's backend handler reads catalog-only data without discovery/introspection recursion. No static scope file supplies startup or package authority. See `components/infra-api/internal/api/{resource_server,resource_mcp,resource_commands,resource_operations}.go`, `cmd/server/{main,proxy_startup}.go`, `internal/resources/catalog.go`, Raptor `internal/{backend/infra_context,catalog/resource_context}.go` and `terraform-module/infra-api-read/{main,proxy}.tf` for source definitions.

The existing logical `cmd/mcp-probe` replaces the obsolete identity-based read probe for current transport checks. Actual release still needs coordinated binary/image/runtime receipts, verified logical relationships and IAM, and read-only qualification. Source parity does not clear replacement skill compatibility or full 2→4→2/native closure gates.
