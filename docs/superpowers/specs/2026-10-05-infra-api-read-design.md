# Infra API: authenticated read integration

Date: 2026-10-05. Status: proposed implementation specification, awaiting user review. Source: current user go-ahead and the agreed whole-POC decisions recorded in `working/design/2026-10-04-infra-api-design.md` in the recovery workspace. This document establishes no implemented endpoint or deployed cloud resource.

## Goal and scope

Build the first independently testable part of Infra API: a Go HTTP service, real read-only cloud adapters, a Raptor HTTP discovery adapter, Serverless Devs FC3 packaging and authenticated Singapore ingress. Prove sandbox → API Gateway → FC → scoped cloud read. This is an incremental acceptance, not the complete infrastructure POC.

Keep all components in the existing public repository. Continue native implementation with meaningful regression tests and one independent final review. Preserve PR #5 and existing legacy prototypes. No API database, operation journal, arbitrary shell endpoint or end-to-end business workflow.

Runtime mutations follow in a separate slice: application restart, proxy scaling, protection and deregistration. Their accepted approval/metrics/capacity gates remain unchanged. The read slice neither exercises nor implements them.

## Service and wire contracts

Independent module `components/infra-api`, Go >=1.25. It does not import Raptor/Gateway storage. Standard net/http routes return `{data:...}` or `{error:{code:...}}` with fixed safe codes. Invalid input: 400; missing/wrong credentials: 401; scope mismatch: 403; changed identity/ambiguous mapping: 409; missing configuration or unavailable provider: 503. Provider failures never become empty successful lists.

* `GET /healthz`: process readiness only, no provider details; available to FC's internal health probe. It is not live provider acceptance.
* `GET /v1/cloud/identity?envCode=...`: authenticated diagnostic STS identity read. Return envCode, configured account match and evidenceMode=live; do not return credentials, tokens or full provider response. Refuse account mismatch.
* `GET /v1/deployments?envCode=...&kind=application|database|db-proxy&code=...`: application discovery uses the selected environment's single ACK cluster and `raptor.appcode`; database discovery matches `env` and `db-code`; ESS proxy discovery matches `env`, `db-proxy-code` and includes `target-db-code`. Results conform to Raptor's existing Deployment JSON shape. Paginate all provider pages with a bounded maximum, failing explicitly on incomplete discovery.
* `GET /v1/deployment-status?envCode=...&appCode=...&clusterId=...&namespace=...&name=...&uid=...`: validate the target's cluster, workload UID and app label before returning the existing UID/generation/observedGeneration/replica status shape. Add allowlisted pod readiness/state without returning pod environment, volumes, secrets or raw manifests. Support Kubernetes Deployment only.

Env/code/namespace/workload inputs have bounded lengths and reject duplicate query fields and unsupported selectors. IDs are opaque strings, not interpolated shell arguments. A valid empty provider response returns an empty array; a missing ACK cluster is NotConfigured, not an invented empty inventory. Responses distinguish simulated fixtures from live observations.

## Scope and environment access

Raptor remains the canonical environment catalog. The agent reads live details through MCP. Infra API separately enforces a deployment-time environment/account/resource allowlist derived from that catalog; it is a permission boundary, not a second environment administration product. It contains no credentials. During this read-only bootstrap, the private allowlist may contain the existing Singapore account with no ACK cluster yet: identity/RDS/ESS reads can work, while ACK routes refuse until a real cluster is registered.

The function does not connect to the Mac's PostgreSQL or pretend the Mac's loopback Raptor endpoint is cloud reachable. No approval callback is needed for read-only calls. Cloud-accessible Raptor and approval verification are prerequisites for the later approval-required mutation slice. An environment edit requiring new resource scope updates the explicit allowlist before the new resource can be read.

FC uses RAM-role temporary credentials through the official credentials provider. No sandbox cloud write credentials. Pin official Alibaba Cloud SDK/credentials dependencies after checking current documentation. RDS/ESS/ACK metadata permissions and Kubernetes list/get permissions are limited to the required reads and selected cluster where provider resource-level scoping is supported. Document any provider action that only permits account-level list scope; enforce exact matching in application code. No mutation actions in the role.

## Authentication and ingress

The user selected Basic Auth. Local direct HTTP uses standard Authorization. For the traditional API Gateway FC3 HTTP backend, official documentation says the gateway overwrites Authorization. Use `X-Infra-Authorization: Basic <encoded username/password>` for caller credentials on that route; the handler accepts only the configured auth-header mode, never an arbitrary client-selected mode. Reject multiple header values, missing credentials and malformed encoding. Compare credentials safely. No credentials in URLs or logs.

Use HTTPS externally. API Gateway invokes an IAM-authenticated FC HTTP trigger through its invocation role; the application's caller credential remains separately checked. Do not select an anonymous trigger as a shortcut. Test valid/wrong/missing caller credentials through the gateway, plus direct function invocation without gateway IAM authorization. If the selected gateway cannot forward the dedicated caller header or invoke the protected function, cloud acceptance fails and the route is revised before publication.

No browser access or CORS permission is required. Raptor backend and agent are HTTP clients. Raptor stores a secret reference in environment configuration; actual service credentials come from private runtime configuration, never MCP context. Disable credential-bearing request/response-body logging.

## Deployment, cost and ownership

Serverless Devs `fc3` owns the function/code/trigger configuration in `components/infra-api/s.yaml`. Prefer a static linux/amd64 Go binary in FC custom runtime, with bounded HTTP timeouts, no reserved warm instances and capped concurrency. Check current runtime/port/start-command fields against FC3 schema before generating configuration.

Terraform owns the gateway, invocation/read-only RAM roles and any required network foundation. Do not use an unverified legacy API Gateway Serverless Devs plugin or manage one resource through two tools.

Candidate ingress is traditional API Gateway, whose official integration explicitly supports FC3 HTTP functions. Before creating it, inspect Singapore account availability and actual selected-edition quote. The published serverless gateway fee is USD 0.0216/hour plus calls/traffic, including idle hours; this does not prove the account's available traditional/shared edition price. Use an initial live probe allocation of at most RMB20 inside the existing cumulative RMB1000 ceiling. If current spending plus the full probe estimate exceeds the ceiling, do not create resources. Keep the live probe short, then delete temporary ingress/function resources using recorded exact IDs unless the user chooses to retain them. No recurring monitoring or billing automation is introduced.

Do not create ACK/RDS/PgCat workloads solely to make this read slice pass. The STS identity path proves the real function role and cloud reachability with existing account resources. Real ACK discovery/status acceptance follows the Terraform foundation stage and remains visibly incomplete until then.

## Tests and acceptance

1. Unit/HTTP tests: credential rejection, dedicated-header gateway request shape, input validation, scope/account mismatch, pagination exhaustion, lookup failure versus true empty discovery, changed workload UID and secret-free output. Use synthetic provider fixtures; no live API call in ordinary tests.
2. Raptor adapter contract: decode discovery/status envelopes, map service errors, retain existing deployment identity and keep no secret in environment context. Preserve unavailable versus empty behavior and legacy suites.
3. Package contract: inspect/build the Serverless Devs artifact for the selected FC runtime; secrets are externally supplied, absent from the package and public config.
4. Live sanitized receipt: deployed source version, Singapore gateway edition/quote, role privilege summary, authenticated sandbox identity read and negative auth/direct-invocation probes. Record live versus simulated evidence and exact cleanup ownership. A health response alone is insufficient.
5. Go race suites/static checks and appropriate existing Node/Python regressions pass. One independent final review before a separate reviewable PR; no automatic merge.

## Alternatives and tradeoffs

Recommended: HTTP web function plus a dedicated caller-auth header. It preserves a normal Go server and the selected FC/API Gateway architecture. An event function can retain event-map headers but needs a separate event transport adapter. Direct public FC HTTP ingress is simpler but does not prove the user's required API Gateway route. Neither alternative is silently substituted.

## Primary sources checked on 2026-10-05

* Serverless Devs overview: https://www.alibabacloud.com/help/en/functioncompute/what-is-serverless-devs
* FC3 schema: https://docs.serverless-devs.com/user-guide/aliyun/fc3/spec/
* Traditional gateway FC3 integration and overwritten Authorization: https://www.alibabacloud.com/help/en/api-gateway/traditional-api-gateway/user-guide/function-compute
* Serverless gateway billing: https://www.alibabacloud.com/help/en/api-gateway/serverless-billing-overview

This specification does not treat the earlier design draft, historical article placeholders or simulated local demo as proof of live infrastructure functionality.
