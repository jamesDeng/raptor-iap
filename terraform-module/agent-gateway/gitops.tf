# Existing shared GitHub OIDC identities are prerequisites, not owned roles.
# This module owns only their Gateway-specific permission supplements.
variable "gitops_state_bucket" {
  type    = string
  default = ""
}
variable "gitops_plan_role" {
  type    = string
  default = ""
}
variable "gitops_apply_role" {
  type    = string
  default = ""
}
locals {
  gitops_roles      = var.gitops_state_bucket == "" ? {} : { plan = var.gitops_plan_role, apply = var.gitops_apply_role }
  owned_role_arns   = [for name in [local.role_name, "raptor-rdev-agent-observer"] : "acs:ram:*:${var.account_id}:role/${name}"]
  shared_role_arns  = [for name in values(local.gitops_roles) : "acs:ram:*:${var.account_id}:role/${name}"]
  owned_policy_arns = [for name in [local.role_name, "raptor-rdev-gateway-ax-checkpoint-write", "raptor-rdev-agent-observer", "raptor-rdev-gateway-assume-observer", "raptor-rdev-gateway-gitops-plan", "raptor-rdev-gateway-gitops-apply"] : "acs:ram:*:${var.account_id}:policy/${name}"]
  # Temporary, exact-resource bootstrap for the separate PAT state. Remove
  # after that component provisions its own GitOps permissions in its state.
  pat_state_bucket_arn = "acs:oss:*:${var.account_id}:${var.gitops_state_bucket}"
  pat_state_arn        = "${local.pat_state_bucket_arn}/raptor-github-pat.ali/terraform.tfstate"
  pat_bucket_arn       = "acs:oss:*:${var.account_id}:raptor-rdev-github-pat-1360282071200743"
  pat_role_arn         = "acs:ram:*:${var.account_id}:role/raptor-rdev-github-pat-backend"
  pat_policy_arn       = "acs:ram:*:${var.account_id}:policy/raptor-rdev-github-pat-backend"
}
resource "terraform_data" "gitops_guard" {
  count = length(local.gitops_roles) == 0 ? 0 : 1
  lifecycle {
    precondition {
      condition     = var.gitops_state_bucket == "raptor-iap-tfstate-sg-200743" && var.gitops_plan_role == "raptor-iap-rdev-plan" && var.gitops_apply_role == "raptor-iap-rdev-apply"
      error_message = "Use only the existing approved POC GitOps identities and state bucket."
    }
  }
}
resource "alicloud_ram_policy" "gitops" {
  for_each    = local.gitops_roles
  policy_name = "raptor-rdev-gateway-gitops-${each.key}"
  force       = false
  policy_document = jsonencode({ Version = "1", Statement = concat([
    { Effect = "Allow", Action = ["sts:GetCallerIdentity"], Resource = ["*"] },
    { Effect = "Allow", Action = ["oss:ListObjects"], Resource = ["acs:oss:*:${var.account_id}:${var.gitops_state_bucket}"], Condition = { StringLike = { "oss:Prefix" = ["gateway.ali/*"] } } },
    { Effect = "Allow", Action = each.key == "apply" ? ["oss:GetObject", "oss:PutObject"] : ["oss:GetObject"], Resource = ["acs:oss:*:${var.account_id}:${var.gitops_state_bucket}/gateway.ali/terraform.tfstate"] },
    { Effect = "Allow", Action = ["ots:DescribeTable", "ots:GetRow", "ots:PutRow", "ots:DeleteRow"], Resource = ["acs:ots:ap-southeast-1:${var.account_id}:instance/raptor-tf-lock/table/terraform_lock"] },
    { Effect = "Allow", Action = ["ram:GetRole", "ram:ListPoliciesForRole"], Resource = concat(local.owned_role_arns, local.shared_role_arns) },
    { Effect = "Allow", Action = ["ram:GetPolicy", "ram:GetPolicyVersion", "ram:ListPolicyVersions", "ram:ListEntitiesForPolicy"], Resource = local.owned_policy_arns },
    { Effect = "Allow", Action = ["oss:ListObjects"], Resource = [local.pat_state_bucket_arn], Condition = { StringLike = { "oss:Prefix" = ["raptor-github-pat.ali/*"] } } },
    { Effect = "Allow", Action = each.key == "apply" ? ["oss:GetObject", "oss:PutObject"] : ["oss:GetObject"], Resource = [local.pat_state_arn] },
    { Effect = "Allow", Action = ["oss:GetBucket*"], Resource = [local.pat_bucket_arn] },
    { Effect = "Allow", Action = ["ram:GetRole", "ram:ListPoliciesForRole"], Resource = [local.pat_role_arn] },
    { Effect = "Allow", Action = ["ram:GetPolicy", "ram:GetPolicyVersion", "ram:ListPolicyVersions", "ram:ListEntitiesForPolicy"], Resource = [local.pat_policy_arn] }
    ], each.key != "apply" ? [] : [
    { Effect = "Allow", Action = ["ram:CreateRole", "ram:UpdateRole"], Resource = local.owned_role_arns },
    { Effect = "Allow", Action = ["ram:CreatePolicy", "ram:CreatePolicyVersion", "ram:DeletePolicyVersion"], Resource = local.owned_policy_arns },
    { Effect = "Allow", Action = ["ram:AttachPolicyToRole"], Resource = concat(local.owned_role_arns, local.shared_role_arns, local.owned_policy_arns) },
    { Effect = "Allow", Action = ["oss:PutBucket", "oss:PutBucketAcl", "oss:PutBucketEncryption"], Resource = [local.pat_bucket_arn] },
    { Effect = "Allow", Action = ["ram:CreateRole", "ram:UpdateRole"], Resource = [local.pat_role_arn] },
    { Effect = "Allow", Action = ["ram:CreatePolicy", "ram:CreatePolicyVersion", "ram:DeletePolicyVersion"], Resource = [local.pat_policy_arn] },
    { Effect = "Allow", Action = ["ram:AttachPolicyToRole"], Resource = [local.pat_role_arn, local.pat_policy_arn] }
  ]) })
  depends_on = [terraform_data.guard, terraform_data.gitops_guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_role_policy_attachment" "gitops" {
  for_each    = local.gitops_roles
  role_name   = each.value
  policy_name = alicloud_ram_policy.gitops[each.key].policy_name
  policy_type = "Custom"
  lifecycle { prevent_destroy = true }
}
