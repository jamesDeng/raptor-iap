# Proposed GitHub planning identity

Status: prepared and tested offline; not created or authorized in Aliyun/GitHub.

Create an OIDC provider named `raptor-iap-github`, trusting `https://token.actions.githubusercontent.com` with audience `sts.aliyuncs.com`. Independently verify its current CA certificate fingerprints at setup time. Do not copy a stale example fingerprint.

Create role `raptor-iap-rdev-plan` and attach custom policy `raptor-iap-rdev-plan-read`. Trust binds the exact subject `repo:jamesDeng/raptor-iap:environment:rdev.ali-plan`, issuer and audience. Configure the GitHub environment `rdev.ali-plan` to permit only branch `main`. Do not run this trusted job on PR source or use pull_request_target to execute PR scripts. Temporary session requests should be 1800 seconds; RAM role maximum is 3600.

The policy allows cloud metadata reads: ECS/VPC/RDS/SLB Describe operations, ACK Get/Describe operations, RAM GetRole/ListRoles/ListPoliciesForRole and STS GetCallerIdentity. Resource `*` permits account-wide metadata visibility for these reads; it is not restricted to POC object IDs. The role cannot create, modify or delete infrastructure, grant IAM permissions, read OSS state objects or change state locks.

This is only the planning identity. Remote-state writes/locking and infrastructure apply require a separately reviewed permission policy. Do not report cloud deployment authentication ready merely because this module validates. Creation and GitHub trust configuration require explicit security approval.

Reviewed source: terraform-module/rdev-plan-identity. Validation and a mocked exact-subject/read-only permission contract pass. No IAM bootstrap module is invoked by the environment module or CI apply.

Sources:
- https://github.com/aliyun/configure-aliyun-credentials-action
- https://www.alibabacloud.com/help/en/ram/user-guide/create-a-ram-role-for-a-trusted-idp
- https://www.alibabacloud.com/help/en/ram/manage-an-oidc-idp
