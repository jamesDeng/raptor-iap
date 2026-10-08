# Dedicated test environment instance (disabled)

Composes reusable test PostgreSQL and PgCat ESS modules. `enabled=false` is the default. This directory has no live backend binding, real resource IDs, credentials, grants, secret bootstrap implementation or workflow changes. It must never share the foundation state key.

The central owner must review: generated catalog associations, Singapore zone/SKU availability, VPC/subnet/security boundary inputs, private database initialization/secret delivery, immutable ECS bootstrap image and the ECS role owned by the PgCat module, ESS/NLB attachment/deregistration behavior, runtime desired-capacity ownership, cost quote and teardown/retention. Then wire a separate state key and PR/GitHub Actions plan/apply. Argo CD owns the separate Kubernetes test charts.

`terraform init -backend=false`, `terraform validate`, and `terraform test` with the checked-in mocked test are offline checks. Do not run cloud plans/applies from this component session. A disabled root composition passing tests is not a deployable secret bootstrap or live stack acceptance.

PgCat owns its configuration-reader role, policy and attachment under terraform-module/pgcat-ess/modules/config-iam. Existing IAM was adopted by the temporary rdev-pgcat-iam entrypoint; transfer its three state entries to this stack before first enabled apply. Never recreate it or leave two states owning it. Required role management is deployment material, not a manual prerequisite.
