# PgCat connection budget correction

Owner-approved bounded policy: use existing Infra API reads for ESS/NLB; MCP Prometheus reads supply PgCat metrics. Alibaba CLI is read-only operator diagnosis. Do not add a service, controller, endpoint, or credential path.

Desired capacity may equal minimum. Keep Terraform-owned minimum2 unchanged for this POC; any minimum configuration change requires approved Terraform/GitOps. Runtime desired-only mutation must never carry MinSize/MaxSize. Reducing desired requires the existing exact one-use approval; raising desired inside valid bounds does not gain a new approval gate.

For the single4→2 removal, sum TOTAL active+idle+waiting over every pool/user on ALL scale-in-eligible nodes; accept combined integer total0or1, reject2+. Missing/stale/incomplete/duplicate/malformed metrics refuse. Protected retained-node clients are excluded from the retiring budget. Preserve withdrawal, app rolling restart, original/replacement lineage, healthy retained capacity, exact approval claims, source freshness and uncertainty/no-retry rules. Healthy registered serving capacity after withdrawal must never be below live minimum; preserve existing additional restrictions.

The accepted one-connection exposure does not establish zero loss. Historical v1/staged evidence keeps its old zero policy. Only the unreleased direct-v2 path adopts budget1; no old result is relabelled PASS.

Deliver as small source/test corrections: API policy, direct harness, direct assessor. Larger evidence-retention or collector redesign needs separate owner approval. No live mutation/deployment in this source correction.
