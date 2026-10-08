# Test foundation GitOps

Owns the existing dedicated PostgreSQL database, zone-B subnet and encrypted versioned configuration bucket. Preserve resource addresses. The backend prefix is fixed to rdev.ali/test-pgcat; never use platform foundation state.

Terraform PRs trigger checks and a protected live plan, followed by a sanitized PR comment. Fork PRs run offline checks only. The plan environment MUST require an owner reviewer and allow same-repository PR runs. Approve only the reviewed exact head: Terraform PR code can execute with plan-role/state-reader permissions. Without this protection, do not enable the workflow. Use branch protection to require review and passing checks before main merge.

Main generates a fresh plan and job summary, uploads only AES-256-GCM encrypted saved-plan bytes, then awaits the rdev.ali-apply environment reviewer. Review that summary and source SHA before approval. Apply decrypts this run’s artifact and executes the exact saved plan. Terraform rejects stale saved plans. Artifact retention is one day; PR plans are not directly applied.

Environment variables: RDEV_PLAN_ROLE_ARN, RDEV_APPLY_ROLE_ARN, RDEV_OIDC_PROVIDER_ARN, RDEV_STATE_BUCKET, RDEV_LOCK_ENDPOINT. Environment secrets: TF_TEST_STACK_INPUTS (JSON with account_id,vpc_id,db_vswitch_id,config_bucket,db_code), TF_PLAN_ENCRYPTION_KEY (same base64-encoded 32 random bytes in plan/apply environments). Never publish these inputs, raw state, plan, logs or keys.

Live enablement is pending reviewed Terraform-managed IAM supplements for scoped test-state access, locking and OSS/NLB reads, environment protection verification, secret configuration and a successful no-change GHA run. No new permissions have been granted by this change. Existing PgCat configuration-reader IAM remains in its owning component module.

For this single-owner POC, self-review may remain allowed so the owner can approve runs they initiated; required reviewer protection is still mandatory.
