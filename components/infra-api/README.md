# Infra API read service

Independent stateless Go HTTP/MCP service. Raptor catalog context owns logical environment/resource relationships; Infra resolves current Alibaba workloads internally. Production startup uses `NewResourceServer` and the explicit resource-mode token inspector. Proxy mutations and restart remain separately opt-in and retain the existing runtime guards.

`make build` creates a static Linux binary in ignored `.build-resource/`. The package directory must contain only `server`; the build refuses symlinks or unexpected contents and never deletes a previous package. Active Serverless templates package this directory. Old `.build/` artifacts are not packaged. No scope JSON file is loaded or bundled.

Set `RAPTOR_CATALOG_ORIGIN` to the fixed HTTPS Raptor backend/API origin, and inject the existing `RAPTOR_SERVICE_USERNAME` / `RAPTOR_SERVICE_PASSWORD` service credentials. This is the catalog-only `/v1/infra/resource-context` route, not an agent MCP endpoint. Catalog context is fetched per operation. Environment catalog fields must specify `cloud=aliyun`, `region=ap-southeast-1`, account and a consistent ACK cluster identity. Proxy replacement additionally requires `proxyApplications` mapping its canonical proxy code to exactly one canonical application code; admission requires exactly one Deployment. Missing or ambiguous data fails closed. No deployment process populates these settings automatically.

`RAPTOR_APPROVAL_ORIGIN` remains the fixed HTTPS approval service origin when proxy commands are enabled. Caller credentials, service credentials and token-introspection credentials are injected privately outside the package. Raptor requests immutable realization evidence through the existing `GET /v1/deployments?envCode=...&kind=db-proxy&code=...` route with `Accept: application/vnd.raptor.resolution-proof+json`. This service-only representation requires the configured authorizer and never introspects an agent; opaque agent credentials are refused before any check or resolver call. Ordinary deployments HTTP/MCP responses remain typed workload envelopes. The separate `/v1/resource-resolution-proof` path is local/internal only and is not required in the deployed API Gateway route inventory. Raptor stores this proof in existing request attempt records and rechecks it before each typed operation and approval claim. No backend replacement-scope file is required. Gateway forwards `X-Infra-Authorization`; the FC trigger remains IAM protected.

Read [the setup guide](../../docs/setup/infra-api-read.md) before release. Source wiring does not authorize a deployment or new IAM permissions. The new NLB discovery reads require release permission review; replacement remains disabled pending coordinated consumer migration and qualification.

Every kubeconfig read requests a fresh 15-minute credential, validates HTTPS/CA, and rejects external credential plugins and local paths. Role permissions do not by themselves establish Kubernetes RBAC: bind get/list Deployments, ReplicaSets and Pods for the selected cluster before live ACK acceptance.

## MCP read transport

`POST /mcp` exposes stateless Streamable HTTP with JSON responses, using MCP Go SDK v1.8.0. The configured caller Basic header is required before initialization, tool discovery or calls; behind API Gateway use `X-Infra-Authorization`. The FC trigger remains IAM-protected. HTTP and MCP share scope validation, selector limits, provider deadlines and safe error codes.

| Tool | Required selectors |
|---|---|
| `deployments_list` | `envCode`, `kind`, `code` |
| `deployment_status_get` | `envCode`, `appCode`, optional `workloadKey` |

Tools return structured `{data: ...}` results. Unknown properties, missing/empty selectors and non-string selectors are rejected. These public selectors are logical resource identities. Discovery returns resource-rooted typed workload envelopes; old cloud-selector calls fail closed. Stateless clients should disable standalone SSE; no GET event stream or persistent MCP session is required.

The subsequent Pi runtime will connect directly to Raptor and Infra API MCP with `direct` exposure. This transport does not itself implement request-specific authorization or a live agent worker. Track **MCP-001** for both catalogs: measure tool-definition tokens, first useful call latency, wrong selections and task success as interfaces grow; evaluate Pi's built-in deferred/tool_search when costs become material. Exposure settings never replace authorization or approval gates.

A model-free probe is available with `go run ./cmd/mcp-probe`. Supply `INFRA_MCP_URL` (HTTPS endpoint ending in `/mcp`), `INFRA_USERNAME`, `INFRA_PASSWORD`, `INFRA_ENV_CODE` and `INFRA_APP_CODE` privately through the environment. It refuses redirects, prints deployment evidence only, and does not print credentials.

References: [API Gateway pass-through rules](https://www.alibabacloud.com/help/en/api-gateway/traditional-api-gateway/user-guide/parameter-mapping-and-verification-rules), [FC web functions](https://www.alibabacloud.com/help/en/functioncompute/web-functions).

### Request-scoped agent authentication

Trusted Raptor backend calls retain service Basic Auth. Sandbox calls instead
use Basic Auth with username `agent` and the opaque, short-lived Raptor attempt
token as password, sent in `X-Infra-Authorization`. The shared Infra service
password must never be staged in a sandbox. HTTP and MCP both introspect every
request and each typed operation; there is no positive authorization cache.
Question attempts expose no mutations. Replacement attempts are restricted to
the resolved environment, proxy/ESS/backend group and associated client
Deployment. Scale-in retains its separate exact human-approval gate.

Set `INFRA_AGENT_INSPECTOR_ORIGIN`, `INFRA_AGENT_INSPECTOR_USERNAME` and
`INFRA_AGENT_INSPECTOR_PASSWORD` privately during FC packaging/deployment.
The latter two match Raptor Open API's dedicated agent-introspection service
credentials; they are distinct from ordinary backend/approval credentials.
No secret belongs in Terraform input/state, source, a PR comment or model input.
Partial inspector configuration fails startup; absent configuration rejects
all sandbox credentials while preserving trusted service calls.

Roll out Infra API and verify a revoked/foreign token is denied first. Then
roll out Gateway's attempt-token transport, with replacement still disabled,
and verify a real read-only application question. Enable replacement only
after the actual sandbox image and approval/checkpoint preflight pass.
