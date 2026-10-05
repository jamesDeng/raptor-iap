# Proposed GitHub planning identity

Status: explicitly authorized and created in Aliyun/GitHub. Trust, sole attached read-only policy and main-only environment branch restriction independently verified. Actual GitHub STS exchange is still untested; run the manual main-only rdev-oidc-check workflow after integration.

Create an OIDC provider named `raptor-iap-github`, trusting `https://token.actions.githubusercontent.com` with audience `sts.aliyuncs.com`. Independently verify its current CA certificate fingerprints at setup time. Do not copy a stale example fingerprint.

Create role `raptor-iap-rdev-plan` and attach custom policy `raptor-iap-rdev-plan-read`. Trust binds the exact subject `repo:jamesDeng/raptor-iap:environment:rdev.ali-plan`, issuer and audience. Configure the GitHub environment `rdev.ali-plan` to permit only branch `main`. Do not run this trusted job on PR source or use pull_request_target to execute PR scripts. Temporary session requests should be 1800 seconds; RAM role maximum is 3600.

The policy allows cloud metadata reads: ECS/VPC/RDS/SLB Describe operations, ACK Get/Describe operations, RAM GetRole/ListRoles/ListPoliciesForRole and STS GetCallerIdentity. Resource `*` permits account-wide metadata visibility for these reads; it is not restricted to POC object IDs. The role cannot create, modify or delete infrastructure, grant IAM permissions, read OSS state objects or change state locks.

This is only the planning identity. Remote-state writes/locking and infrastructure apply require a separately reviewed permission policy. Do not report cloud deployment authentication ready merely because this module validates. Creation and GitHub trust configuration require explicit security approval.

Reviewed source: terraform-module/rdev-plan-identity. Validation and a mocked exact-subject/read-only permission contract pass. No IAM bootstrap module is invoked by the environment module or CI apply.

Sources:
- https://github.com/aliyun/configure-aliyun-credentials-action
- https://www.alibabacloud.com/help/en/ram/user-guide/create-a-ram-role-for-a-trusted-idp
- https://www.alibabacloud.com/help/en/ram/manage-an-oidc-idp

The approved initial identity was created via Aliyun CLI and verified through independent read APIs. The Terraform module is the source definition but has not been imported into a live identity-bootstrap state. Before running this module against the account, import the existing provider, role, custom policy and attachment into protected bootstrap state; inspect a no-change plan. Do not apply it blindly or recreate existing identities.

The planning policy explicitly denies ACK kubeconfig retrieval, cluster attach scripts and Kubernetes trigger details. These exceptions prevent broad metadata wildcards from retrieving credentials or bootstrap material. The live default policy was narrowed and read back on 2026-10-05.
