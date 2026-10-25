# Component-owned, read-only access for its PR retirement plans. Removed with
# this component; no new role, cloud mutation grant or credential is provisioned.
resource "alicloud_ram_policy" "gitops_plan" {
  policy_name = "raptor-ax-sandbox-retirement-plan"
  force       = false
  policy_document = jsonencode({
    Version = "1"
    Statement = [
      { Effect = "Allow", Action = ["oss:ListObjects"], Resource = ["acs:oss:*:${var.account_id}:raptor-iap-tfstate-sg-200743"], Condition = { StringLike = { "oss:Prefix" = ["ax-sandbox.ali/*"] } } },
      { Effect = "Allow", Action = ["oss:GetObject"], Resource = ["acs:oss:*:${var.account_id}:raptor-iap-tfstate-sg-200743/ax-sandbox.ali/terraform.tfstate"] },
      { Effect = "Allow", Action = ["oss:GetBucketInfo", "oss:GetBucketLocation"], Resource = ["acs:oss:*:${var.account_id}:raptor-iap-tfstate-sg-200743"] },
      { Effect = "Allow", Action = ["ram:GetPolicy", "ram:GetPolicyVersion"], Resource = ["acs:ram:*:${var.account_id}:policy/raptor-ax-sandbox-retirement-plan"] }
    ]
  })
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_ram_role_policy_attachment" "gitops_plan" {
  role_name   = "raptor-iap-rdev-plan"
  policy_name = alicloud_ram_policy.gitops_plan.policy_name
  policy_type = "Custom"
}
