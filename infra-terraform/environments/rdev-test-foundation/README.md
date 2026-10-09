# Test foundation GitOps

Owns the existing dedicated PostgreSQL database, zone-B subnet and encrypted versioned configuration bucket. Preserve resource addresses. The backend prefix is fixed to rdev.ali/test-pgcat; never use platform foundation state.

Terraform PRs trigger checks and an automatic live plan, followed by a sanitized PR comment. Fork PRs run offline checks only. Same-repository PR plans run without a reviewer gate; fork PRs remain offline. PR Terraform code can execute with plan-role/state-reader permissions. Use branch protection to require review and passing checks before main merge.

Main generates a fresh plan and job summary, uploads only AES-256-GCM encrypted saved-plan bytes, then awaits the rdev.ali-apply environment reviewer. Review that summary and source SHA before approval. Apply decrypts this run’s artifact and executes the exact saved plan. Terraform rejects stale saved plans. Artifact retention is one day; PR plans are not directly applied.

Environment variables: RDEV_PLAN_ROLE_ARN, RDEV_APPLY_ROLE_ARN, RDEV_OIDC_PROVIDER_ARN, RDEV_STATE_BUCKET, RDEV_LOCK_ENDPOINT. Environment secrets: TF_TEST_STACK_INPUTS (JSON with account_id,vpc_id,db_vswitch_id,config_bucket,db_code), TF_PLAN_ENCRYPTION_KEY (same base64-encoded 32 random bytes in plan/apply environments). Never publish these inputs, raw state, plan, logs or keys.

The foundation GitOps workflow has been enabled and verified independently. This fleet extension has not been applied.

## Optional PgCat fleet

Set proxy_enabled and bind proxy_code, proxy_worker_vswitch_id and proxy_secret_version through protected private inputs. The module owns its security group, ESS, private NLB and configuration-reader IAM. Two baseline ECS nodes share the existing zone-A subnet/NAT; NLB uses zones A and B. ECS resilience is therefore single-zone for this POC. Credentials remain in the version-pinned private OSS object, outside Terraform source and state.

Before the enabled fleet apply, apply the retirement configuration in rdev-pgcat-iam. Its removed block with destroy=false releases the old state ownership without deleting IAM. Then the fleet imports the existing role, policy and attachment without changing them. Never leave both states managing the same IAM resources.

The bootstrap artifact is reviewed, but live ECS first-boot and SQL through the NLB remain deployment acceptance checks. The separate metadata-read supplement requires owner approval before granting it; fleet creation permissions must also be reviewed before apply.

For this single-owner POC, self-review remains allowed; apply environment reviewer protection is mandatory. Main branch protection is not currently configured.
