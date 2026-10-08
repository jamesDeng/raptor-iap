---
name: pgcat-deploy
description: Use when preparing or reviewing PgCat deployment for a selected Raptor database-proxy object on Aliyun ECS managed by ESS, or reconciling an interrupted proxy provisioning request.
---

# PgCat proxy deployment

Draft reference. Prepare reviewed permanent configuration and authorized read-only preflight; do not provision until actual command/workflow, immutable bootstrap image/secret delivery, permissions, private network and cost contracts are centrally reviewed. See [runtime enablement](../pgcat-replacement/references/contracts.md).

Read selected request/attempt, proxy code, environment, target database code, desired capacity, instance class, PgCat version and resolved skill commit. A proxy serves exactly one database in the SAME environment. A database may have multiple proxies; never infer unique proxy or process count from that relationship. Discover the DB via `env`/`db-code` and proxy ESS via `env`/`db-proxy-code`; require proxy `target-db-code` to match. Missing, ambiguous, cross-environment or changed identities block preparation of dependent actions. Never substitute platform RDS or use a newly generated catalog code without request review.

Use reusable `terraform-module/pgcat-ess/` and separate environment instances. The initial test module bounds baseline two / maximum four. Permanent changes go through Terraform PR and centrally owned Actions; ACK app/Prometheus definitions go through GitOps/Argo CD. Record reviewed commit, workflow run, provider group/backend identities and pinned skill SHA; a merged PR is not deployment success.

Central image review must supply an immutable ECS image and bootstrap revision, existing narrowly scoped secret-read role and selected private secret reference. Bootstrap must fetch/render credentials privately, verify config revision and DB/TLS readiness, then start PgCat/metrics. Do not put passwords in user data, Terraform state/plan, Git, images, logs or model parameters. `bootstrap_reviewed=false` blocks the module; this branch has no implemented/qualified credential bootstrap image. Do not create grants as a workaround.

Require private VPC/subnets/NLB and security groups, verified Singapore SKU/zones, ESS NLB membership behavior and direct private Prometheus scrape inventory by ECS node identity. TCP-only health does not prove SQL readiness. Do not enable replacement until client/backend TLS, SQL traffic, metrics active+idle+waiting across expected static pools, source freshness, app-to-proxy associations and actual Infra API gates are qualified. Linux image/container qualification remains separate from source-built macOS local tests.

On pending/failed/unknown apply inspect the recorded Actions run and authoritative ownership/resource state before any retry, new create or import. Do not reset live ESS desired capacity during replacement: Terraform narrowly leaves runtime desired-capacity changes to the API, while drift and final baseline restoration are explicitly reviewed. Unexpected auto-registration of drained nodes pauses replacement for central reconciliation.

Report separately: local mocked plan/configuration tests; reviewed PR/apply outcome; live same-environment DB/proxy discovery; private client/TLS/readiness/metrics evidence; actual spend. Retain bounded rollout/retention/cleanup decisions centrally. Whole-folder skill version is pinned to this request; no mutable tags or silent instruction changes.
