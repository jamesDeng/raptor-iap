variable "account_id" { type = string }
variable "state_bucket" { type = string }
variable "config_bucket" { type = string }
variable "plan_role_name" { type = string }
variable "apply_role_name" { type = string }
locals {
  roles = { plan = var.plan_role_name, apply = var.apply_role_name }
}
data "alicloud_account" "current" {}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Wrong GitOps deployment account."
    }
  }
}
resource "alicloud_ram_policy" "state" {
  for_each    = local.roles
  policy_name = "raptor-iap-test-gitops-${each.key}"
  force       = false
  policy_document = jsonencode({
    Version = "1"
    Statement = [
      { Effect = "Allow", Action = ["oss:ListObjects"], Resource = ["acs:oss:*:${var.account_id}:${var.state_bucket}"], Condition = { StringLike = { "oss:Prefix" = ["rdev.ali/test-pgcat/*"] } } },
      { Effect = "Allow", Action = each.key == "apply" ? ["oss:GetObject", "oss:PutObject"] : ["oss:GetObject"], Resource = ["acs:oss:*:${var.account_id}:${var.state_bucket}/rdev.ali/test-pgcat/terraform.tfstate"] },
      { Effect = "Allow", Action = ["oss:GetBucket*"], Resource = ["acs:oss:*:${var.account_id}:${var.config_bucket}"] },
      { Effect = "Allow", Action = ["ots:DescribeTable", "ots:GetRow", "ots:PutRow", "ots:DeleteRow"], Resource = ["acs:ots:ap-southeast-1:${var.account_id}:instance/raptor-tf-lock/table/terraform_lock"] }
    ]
  })
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_ram_role_policy_attachment" "state" {
  for_each    = local.roles
  role_name   = each.value
  policy_name = alicloud_ram_policy.state[each.key].policy_name
  policy_type = "Custom"
}

# Read-only fleet planning supplement. No credential-object reads or cloud writes.
resource "alicloud_ram_policy" "fleet_read" {
  policy_name = "raptor-iap-test-gitops-fleet-read"
  force       = false
  policy_document = jsonencode({
    Version = "1"
    Statement = [
      { Effect = "Allow", Action = ["ess:DescribeScalingGroups", "ess:DescribeScalingConfigurations", "ess:DescribeScalingInstances", "ess:DescribeScalingActivities", "ess:ListTagResources", "nlb:ListLoadBalancers", "nlb:GetLoadBalancerAttribute", "nlb:ListServerGroups", "nlb:GetServerGroupAttribute", "nlb:ListServerGroupServers", "nlb:ListListeners", "nlb:GetListenerAttribute", "nlb:ListTagResources", "ram:ListTagResources"], Resource = ["*"] },
      { Effect = "Allow", Action = ["ram:GetPolicy", "ram:GetPolicyVersion"], Resource = ["acs:ram::${var.account_id}:policy/raptor-iap-rdev-pgcat-config-read"] }
    ]
  })
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_ram_role_policy_attachment" "fleet_read" {
  for_each    = local.roles
  role_name   = each.value
  policy_name = alicloud_ram_policy.fleet_read.policy_name
  policy_type = "Custom"
}
