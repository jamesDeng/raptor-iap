data "alicloud_account" "current" {}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Authenticated account does not match identity bootstrap account."
    }
  }
}
resource "alicloud_ims_oidc_provider" "github" {
  oidc_provider_name = "raptor-iap-github"
  issuer_url         = "https://token.actions.githubusercontent.com"
  client_ids         = ["sts.aliyuncs.com"]
  fingerprints       = var.fingerprints
  depends_on         = [terraform_data.account_guard]
}
resource "alicloud_ram_role" "plan" {
  role_name            = "raptor-iap-rdev-plan"
  force                = false
  max_session_duration = 3600
  assume_role_policy_document = jsonencode({
    Version = "1"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Federated = alicloud_ims_oidc_provider.github.arn }
      Condition = { StringEquals = {
        "oidc:iss" = "https://token.actions.githubusercontent.com"
        "oidc:aud" = "sts.aliyuncs.com"
        "oidc:sub" = "repo:jamesDeng@4443650/raptor-iap@1397422754:environment:rdev.ali-plan"
      } }
    }]
  })
}
resource "alicloud_ram_policy" "plan" {
  policy_name = "raptor-iap-rdev-plan-read"
  force       = false
  policy_document = jsonencode({
    Version = "1"
    Statement = [{
      Effect   = "Allow"
      Action   = ["ecs:Describe*", "vpc:Describe*", "rds:Describe*", "cs:Describe*", "cs:Get*", "slb:Describe*", "ram:GetRole", "ram:ListRoles", "ram:ListPoliciesForRole", "sts:GetCallerIdentity"]
      Resource = "*"
      }, {
      Effect   = "Deny"
      Action   = ["cs:DescribeClusterUserKubeconfig", "cs:DescribeClusterV2UserKubeconfig", "cs:DescribeClusterAttachScripts", "cs:GetKubernetesTrigger"]
      Resource = "*"
    }]
  })
}
resource "alicloud_ram_role_policy_attachment" "plan" {
  role_name   = alicloud_ram_role.plan.role_name
  policy_name = alicloud_ram_policy.plan.policy_name
  policy_type = "Custom"
}
