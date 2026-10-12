# Environment repositories and sandbox Git acceptance

## Deployment gate

The code path is disabled by default (`repositoryChange.enabled: false`). Keep it disabled until the PAT store Terraform PR posts a sanitized plan comment, its reviewed apply completes, the Gateway and Raptor images containing this change are deployed, a reviewed `skills-v*` tag contains `agent-skills/repository-change/SKILL.md`, and the owner saves a scoped personal PAT through Raptor Admin. Do not put the PAT in Helm values, a Secret manifest, Terraform variables, or source.

The PAT stack uses a separate `raptor-github-pat.ali/terraform.tfstate` object. Its PR plan needs the exact state read permission on the shared GitOps plan role before it can initialize. Prerequisite PR #150 adds temporary, exact-resource access through the already managed Gateway GitOps policy; confirm its main-branch reviewed apply succeeded before retrying the PAT plan. The `TF_RAPTOR_GITHUB_PAT_STACK_INPUTS` GitHub secret contains only the verified rdev OIDC provider ARN and issuer. Remove the temporary supplement after the PAT component has its own GitOps permissions.

Use the existing reviewed apply workflow for Terraform. Do not apply the module manually. Do not enable `repositoryChange` without `githubPAT` and an AX live runtime.

## Local evidence

The feature branch's local checks cover Raptor and Gateway Go suites against a disposable PostgreSQL instance; pinned AX bridge and Workspace Git tests; Pi harness Node tests; Helm rendering with the feature off and on; and Terraform formatting, validation, and mock module tests. Record the exact commit and workflow run in the rollout ticket. These checks establish code behavior only.

## Read-only deployed checks

1. Audit legacy environment `config` values and compare structured columns, every repository row, environment settings, and synthesized API output using `docs/setup/environment-storage-audit.md`. Record redacted counts and sample IDs.
2. Verify the Admin hostname's served TLS certificate SAN. Confirm anonymous and ordinary-user requests cannot read or change the PAT setting.
3. Confirm Admin PAT status exposes owner label and verification time, without token bytes. Save and replace a scoped PAT through the Admin UI; confirm the previous secret reference is retired.
4. Confirm the selected environment lists every intended public GitHub repository, directory, and base branch. Rows that share a URL must share a branch.

## Sandbox acceptance

1. Start one harmless `repository.change` request. Record request and attempt IDs, the selected environment, and the immutable repository snapshot. Verify every row is attached, duplicate URLs produce one clone, and every origin and HEAD matches its stored base commit before Pi begins.
2. Confirm Pi uses the `repository-change` skill to select a repository by purpose and directory. Verify the question operation still exposes only its original read-only tools and fixed workspace.
3. Let Pi make a harmless edit, run focused checks, push a unique working branch, open a PR against the configured base branch, and call `pull_request_attach`. Verify Raptor stores the matching request, attempt, repository, base commit, base branch, working branch, PR number, URL, and head SHA. Attempt an unrelated request or repository association and verify rejection.
4. Read `GET /api/v1/requests/{id}/pull-request-actions` as an authorized browser user. Confirm it reports pending, success, failure, or unavailable for the PR head SHA without keeping the sandbox alive. No workflow rerun, merge, or Terraform apply is part of this acceptance.
5. Cancel a separate attempt during inference and checkpointing. Verify AX Task and Workspace deletion, agent credential revocation, and absence of PAT bytes in archived state. PAT can be deliberately read by Pi through the accepted sandbox helper; treat that as an owner-approved exposure, and rotate the PAT if such output appears.

## Evidence still needed

- No live data migration audit or deployed Admin TLS/authorization check has been performed for this branch.
- No PAT has been submitted, no sandbox attempt or PR has been created, and no GitHub Actions state has been observed in rdev for this branch.
- No Terraform plan comment or reviewed apply has run for this branch.
