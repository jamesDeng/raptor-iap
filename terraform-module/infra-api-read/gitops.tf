# Shared provider identities already exist. This component owns only its
# deployment supplements; it does not create or change their trust policies.
locals {
  gitops_roles          = var.gitops_plan_role == "" ? {} : { plan = var.gitops_plan_role, apply = var.gitops_apply_role }
  component_policy_arns = [for name in ["${var.name}-read", "${var.name}-invoke", "${var.name}-gitops-plan", "${var.name}-gitops-apply"] : "acs:ram:*:${var.account_id}:policy/${name}"]
  component_role_arns   = [for name in concat(["${var.name}-read", "${var.name}-invoke"], values(local.gitops_roles)) : "acs:ram::${var.account_id}:role/${name}"]
  component_group_arn   = "acs:apigateway:${var.region}:${var.account_id}:apigroup/${alicloud_api_gateway_group.read.id}"
}
resource "alicloud_ram_policy" "gitops" {
  for_each    = local.gitops_roles
  depends_on  = [terraform_data.account_guard]
  policy_name = "${var.name}-gitops-${each.key}"
  force       = false
  policy_document = jsonencode({ Version = "1", Statement = concat([
    { Effect = "Allow", Action = ["sts:GetCallerIdentity"], Resource = ["*"] },
    { Effect = "Allow", Action = ["ram:GetRole", "ram:ListPoliciesForRole"], Resource = local.component_role_arns },
    { Effect = "Allow", Action = ["ram:GetPolicy", "ram:GetPolicyVersion", "ram:ListPolicyVersions", "ram:ListEntitiesForPolicy"], Resource = local.component_policy_arns },
    { Effect = "Allow", Action = ["apigateway:DescribeApiGroup", "apigateway:DescribeApiGroupDetail", "apigateway:DescribeApi", "apigateway:DescribeDeployedApi"], Resource = [local.component_group_arn] },
    { Effect = "Allow", Action = ["oss:ListObjects"], Resource = ["acs:oss:*:${var.account_id}:${var.gitops_state_bucket}"], Condition = { StringEquals = { "oss:Prefix" = ["rdev.ali/"] } } },
    { Effect = "Allow", Action = each.key == "apply" ? ["oss:GetObject", "oss:PutObject"] : ["oss:GetObject"], Resource = ["acs:oss:*:${var.account_id}:${var.gitops_state_bucket}/rdev.ali/infra-api.tfstate"] }
    ], each.key != "apply" ? [] : [
    { Effect = "Allow", Action = ["ram:CreatePolicyVersion", "ram:DeletePolicyVersion"], Resource = local.component_policy_arns },
    { Effect = "Allow", Action = ["apigateway:CreateApi", "apigateway:ModifyApi", "apigateway:DeployApi"], Resource = [local.component_group_arn] },
    { Effect = "Allow", Action = ["ram:PassRole"], Resource = [alicloud_ram_role.invoke.arn] }
  ]) })
}
resource "alicloud_ram_role_policy_attachment" "gitops" {
  for_each    = local.gitops_roles
  role_name   = each.value
  policy_name = alicloud_ram_policy.gitops[each.key].policy_name
  policy_type = "Custom"
}
