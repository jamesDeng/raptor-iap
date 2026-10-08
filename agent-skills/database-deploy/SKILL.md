---
name: database-deploy
description: Use when preparing or reviewing deployment of a selected Raptor PostgreSQL database object in Aliyun Singapore, or reconciling an interrupted Terraform database deployment request.
---

# Dedicated PostgreSQL deployment

Draft reference. Only prepare a reviewable PR/configuration and perform authorized read-only inspection until central backend/workflow, DB account/secret/schema initialization and exact tool bindings are reviewed. This skill is not permission to provision or spend money.

Read the selected request with its existing database code, environment, request/attempt IDs, module inputs and pinned skills commit. Discover current database deployments using `env` and `db-code`. An existing platform RDS is not this test DB; do not reuse it silently. Require same account/Singapore region and unambiguous catalog identity. If the same target already exists, reconcile its Terraform ownership and actual state; do not create another DB or auto-import into another session's state.

Use `terraform-module/test-postgresql/` for reusable resources; environment instances live under `infra-terraform/environments/`. The disabled `rdev-test-pgcat` composition is an example, not a live backend. Supply explicit reviewed class/version/storage/zone, existing private network IDs and bounded private client CIDRs; keep auto storage expansion disabled. Never include DB passwords in configuration, Git, plans, logs or outputs. The module does not create accounts/databases/tables: private credential delivery and initialization require their own reviewed path.

Prepare permanent changes through Terraform PR review and centrally owned GitHub Actions. Pin the exact module/repository revision and record PR, reviewed commit, workflow/run and target identities on the request. Inspect a successful apply and live discovery/DB readiness before reporting deployment complete. Saved-plan account identity is checked again at apply; permissions and budget still need concrete central approval.

On failed/pending/unknown Actions results, inspect the recorded run and authoritative resource/state ownership read-only before retry. Never submit another create/apply blindly or infer success from a merged PR. A changed plan must be reviewed again; state lock/import/grants are not automatic recovery actions. Report provider errors and uncertain ownership truthfully.

Acceptance separates: local mocked module contracts; reviewed PR/Actions result; live dedicated DB resource discovery; private TLS/auth/schema readiness; actual cost. Zero application failures and agent runtime success require separate evidence. Whole-folder skill tag and exact SHA stay attached to this request; later tags cannot silently change its instructions.
