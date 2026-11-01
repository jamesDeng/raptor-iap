# The shared OIDC roles are prerequisites. This component owns only their
# exact AX state and instance permissions; no role is created in an environment root.
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
  gitops_roles = var.gitops_state_bucket == "" ? {} : {
    plan  = var.gitops_plan_role
    apply = var.gitops_apply_role
  }
  gitops_policy_arns = [for kind in keys(local.gitops_roles) : "acs:ram:*:1360282071200743:policy/raptor-ecs-ax-gitops-${kind}"]
  gitops_role_arns   = [for name in values(local.gitops_roles) : "acs:ram:*:1360282071200743:role/${name}"]
}

resource "terraform_data" "gitops_guard" {
  count = length(local.gitops_roles) == 0 ? 0 : 1
  lifecycle {
    precondition {
      condition     = var.gitops_state_bucket == "raptor-iap-tfstate-sg-200743" && var.gitops_plan_role == "raptor-iap-rdev-plan" && var.gitops_apply_role == "raptor-iap-rdev-apply"
      error_message = "AX GitOps requires the existing reviewed POC roles and state bucket."
    }
  }
}

resource "alicloud_ram_policy" "gitops" {
  for_each    = local.gitops_roles
  policy_name = "raptor-ecs-ax-gitops-${each.key}"
  force       = false
  policy_document = jsonencode({ Version = "1", Statement = concat([
    { Effect = "Allow", Action = ["oss:ListObjects"], Resource = ["acs:oss:*:1360282071200743:${var.gitops_state_bucket}"], Condition = { StringLike = { "oss:Prefix" = ["ecs-ax.ali/*"] } } },
    { Effect = "Allow", Action = each.key == "apply" ? ["oss:GetObject", "oss:PutObject"] : ["oss:GetObject"], Resource = ["acs:oss:*:1360282071200743:${var.gitops_state_bucket}/ecs-ax.ali/terraform.tfstate"] },
    { Effect = "Allow", Action = ["ots:DescribeTable", "ots:GetRow", "ots:PutRow", "ots:DeleteRow"], Resource = ["acs:ots:ap-southeast-1:1360282071200743:instance/raptor-tf-lock/table/terraform_lock"] },
    { Effect = "Allow", Action = ["ram:GetPolicy", "ram:GetPolicyVersion", "ram:ListPolicyVersions", "ram:ListEntitiesForPolicy"], Resource = local.gitops_policy_arns },
    { Effect = "Allow", Action = ["ram:GetRole", "ram:ListPoliciesForRole"], Resource = local.gitops_role_arns }
    ], each.key == "apply" ? [
    { Effect = "Allow", Action = ["ecs:StopInstance", "ecs:StartInstance", "ecs:ModifyInstanceSpec"], Resource = ["acs:ecs:ap-southeast-1:1360282071200743:instance/i-t4nj9k37b82g5a64x424"] },
    { Effect = "Allow", Action = ["ram:CreatePolicyVersion", "ram:DeletePolicyVersion"], Resource = local.gitops_policy_arns },
    { Effect = "Allow", Action = ["ram:AttachPolicyToRole"], Resource = concat(local.gitops_policy_arns, local.gitops_role_arns) }
  ] : []) })
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
