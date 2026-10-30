# PgCat Infra MCP read routing — owner-approved bounded integration

Status: owner explicitly approved this bounded integration on2026-10-30. No API/routing implementation or cloud changes have yet been performed.

## Corrected current facts
The existing Infra API exposes identity, deployments and app status, and has internal Alibaba SDK reads for mutation guards. It does not expose the complete ESS/NLB observation contract. The normal Gateway observer translates CLI-shaped requests into direct Alibaba SDK calls; it does not call Infra API for these reads. Its custom PgCat metrics tool queries Prometheus over private HTTP, rather than through an Infra MCP read tool. Earlier statements that these complete MCP read paths already existed were incorrect.

## Proposed bounded change
Use the EXISTING Infra API service and EXISTING /mcp endpoint. Add only two fixed read tools (names proposed):

1. proxy_observation_get: exact request/environment/proxy/group/backend scope and kind fleet, capacity or backend. Reuse existing Alibaba SDK identity/clients and validate pagination/target identity. Return the same sanitized fields already consumed by the agent, including pending lifecycle, protection, health and NLB private IP/status. Keep the stricter mutation snapshot unchanged.
2. proxy_pgcat_metrics_get: same exact scope plus one of eight fixed PgCat series selections (up and timestamp, active/idle/waiting and their timestamps). Reuse existing Infra private Prometheus query transport; reject arbitrary URLs, PromQL, metric names or resources. Current all-pool completeness/freshness validator remains the consumer.

Route normal Gateway reads through these tools, including nested cloud reads in PgCat metrics and SQL probes. Preserve model-facing cloud_read/metrics_read/db_connection_probe output contracts. Reuse the current attempt credential and introspection with exact read allowlists in Infra API and Raptor; credentials stay private. No automatic fallback to direct cloud/Prometheus reads in the normal path. Alibaba CLI remains an operator diagnostic option.

## Explicit limits
No new service, cloud resource, RAM action, credential scheme, arbitrary-query endpoint, controller, collector/evidence framework or model tool catalog. No deletion of current code/resources. No new public HTTP API Gateway route or Terraform provisioning. Deployment read permissions and private metrics access must be verified before live use.

## Size and verification
Estimated medium cross-component change:12–16 production files and roughly550–900 readable changed lines, plus8–11 test files. This is an estimate, not implemented code. Components: Infra API read projections/MCP/auth, Raptor read selectors, Gateway/harness private transport. Stop and return for review if new cloud permissions/resources or materially broader scope becomes necessary.

Tests must cover wrong/expired/revoked/application scope refusal; pending/partially converged fleet and backend semantics; all-page reads; complete pool metrics and fixed series only; unchanged guard snapshots; no direct-cloud/CLI fallback including nested probes; same output contracts and existing deadlines; no credential disclosure. Source tests/review precede GitOps release and live verification.

## Separation from existing work
The connection-budget correction (combined total active+idle+waiting<=1, live minimum capacity floor, desired>=minimum, existing scale-in approval) is already implemented/reviewed separately at1c71b27; current CI is separate. This proposal does not finish the full2→4→2 POC or authorize a larger test-controller redesign.

## Approval requested
Owner approved this precise medium read-routing integration before source implementation. Material scope expansion, new cloud resources/IAM or controller work still requires approval.
