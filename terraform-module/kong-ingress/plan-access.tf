# Bootstrap with a reviewed, targeted Terraform plan using the deployment
# identity. The PR planning identity cannot read this stack's state until this
# policy and attachment exist; never give the PR job the apply identity.
data "alicloud_account" "plan_access" {
  count = var.enable_plan_access ? 1 : 0
}
resource "terraform_data" "plan_access_account_guard" {
  count = var.enable_plan_access ? 1 : 0
  lifecycle {
    precondition {
      condition     = data.alicloud_account.plan_access[0].id == var.account_id
      error_message = "Wrong account for Kong planning access."
    }
  }
}
resource "alicloud_ram_policy" "plan_access" {
  count       = var.enable_plan_access ? 1 : 0
  policy_name = "raptor-rdev-kong-gitops-plan"
  force       = false
  policy_document = jsonencode({
    Version = "1"
    Statement = [
      { Effect = "Allow", Action = ["oss:ListObjects"], Resource = ["acs:oss:*:${var.account_id}:${var.state_bucket}"], Condition = { StringLike = { "oss:Prefix" = ["rdev.ali/*"] } } },
      { Effect = "Allow", Action = ["oss:GetObject"], Resource = ["acs:oss:*:${var.account_id}:${var.state_bucket}/rdev.ali/terraform.tfstate"] },
      { Effect = "Allow", Action = ["ots:DescribeTable", "ots:GetRow", "ots:PutRow", "ots:DeleteRow"], Resource = ["acs:ots:ap-southeast-1:${var.account_id}:instance/raptor-tf-lock/table/terraform_lock"] },
      { Effect = "Allow", Action = ["alidns:DescribeDomainRecords", "alidns:DescribeDomainRecordInfo"], Resource = ["acs:alidns::${var.account_id}:domain/raptor-iap.top"] },
      { Effect = "Allow", Action = ["vpc:ListTagResources"], Resource = ["*"] },
      { Effect = "Allow", Action = ["ram:GetPolicy", "ram:GetPolicyVersion", "ram:ListPolicyVersions", "ram:ListEntitiesForPolicy"], Resource = ["acs:ram:*:${var.account_id}:policy/raptor-rdev-kong-gitops-plan"] }
    ]
  })
  depends_on = [terraform_data.plan_access_account_guard]
}
resource "alicloud_ram_role_policy_attachment" "plan_access" {
  count       = var.enable_plan_access ? 1 : 0
  role_name   = "raptor-iap-rdev-plan"
  policy_name = alicloud_ram_policy.plan_access[0].policy_name
  policy_type = "Custom"
}
