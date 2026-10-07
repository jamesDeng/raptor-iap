# Infra API read service

Independent stateless Go HTTP service. Discovery reads STS/RDS/ESS and registered ACK Deployment status. Missing providers/cluster configuration return errors rather than successful empty inventories. This slice has no mutations.

Read [the setup guide](../../docs/setup/infra-api-read.md) before cloud deployment. Supply private scope JSON (array of code/accountId/region/optional clusterId) and caller credentials through private runtime configuration. Serverless Devs owns FC3 code/function/HTTP trigger; Terraform owns RAM roles and API Gateway. Gateway forwards `X-Infra-Authorization`, while FC's `authType: function` requires IAM signatures.

`make build INFRA_SCOPE_FILE=/private/path/scope.json` builds a static Linux binary and copies the credential-free private scope. `.build` is ignored. Caller credentials are not in the code package; they are externally injected in the function configuration. Do not publish rendered Serverless configuration or logs.

Every kubeconfig read requests a fresh 15-minute credential, validates HTTPS/CA, and rejects external credential plugins and local paths. Role permissions do not by themselves establish Kubernetes RBAC: bind get/list Deployments, ReplicaSets and Pods for the selected cluster before live ACK acceptance.

## MCP read transport

`POST /mcp` exposes stateless Streamable HTTP with JSON responses, using MCP Go SDK v1.8.0. The configured caller Basic header is required before initialization, tool discovery or calls; behind API Gateway use `X-Infra-Authorization`. The FC trigger remains IAM-protected. HTTP and MCP share scope validation, selector limits, provider deadlines and safe error codes.

| Tool | Required selectors |
|---|---|
| `cloud_identity_get` | `envCode` |
| `deployments_list` | `envCode`, `kind`, `code` |
| `deployment_status_get` | `envCode`, `appCode`, `clusterId`, `namespace`, `name`, `uid` |

Tools return structured `{data: ...}` results. Unknown properties, missing/empty selectors and non-string selectors are rejected. These are read operations only. Existing HTTP endpoints remain unchanged. Stateless clients should disable standalone SSE; no GET event stream or persistent MCP session is required.

The subsequent Pi runtime will connect directly to Raptor and Infra API MCP with `direct` exposure. This transport does not itself implement request-specific authorization or a live agent worker. Track **MCP-001** for both catalogs: measure tool-definition tokens, first useful call latency, wrong selections and task success as interfaces grow; evaluate Pi's built-in deferred/tool_search when costs become material. Exposure settings never replace authorization or approval gates.

A model-free probe is available with `go run ./cmd/mcp-probe`. Supply `INFRA_MCP_URL` (HTTPS endpoint ending in `/mcp`), `INFRA_USERNAME`, `INFRA_PASSWORD`, `INFRA_ENV_CODE` and `INFRA_APP_CODE` privately through the environment. It refuses redirects, prints deployment evidence only, and does not print credentials.

References: [API Gateway pass-through rules](https://www.alibabacloud.com/help/en/api-gateway/traditional-api-gateway/user-guide/parameter-mapping-and-verification-rules), [FC web functions](https://www.alibabacloud.com/help/en/functioncompute/web-functions).
