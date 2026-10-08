data "alicloud_account" "current" {}
resource "terraform_data" "guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Gateway account boundary mismatch."
    }
    precondition {
      condition     = !var.rrsa_enabled || (var.oidc_provider_arn == "acs:ram::${var.account_id}:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1" && startswith(var.oidc_issuer, "https://"))
      error_message = "Verified original ACK RRSA issuer/provider required."
    }
  }
}
locals {
  role_name      = "raptor-rdev-gateway-controller"
  adoption_trust = { Version = "1", Statement = [{ Effect = "Allow", Action = "sts:AssumeRole", Principal = { RAM = ["acs:ram::${var.account_id}:root"] } }] }
  rrsa_trust     = { Version = "1", Statement = [{ Effect = "Allow", Action = "sts:AssumeRoleWithOIDC", Principal = { Federated = [var.oidc_provider_arn] }, Condition = { StringEquals = { "oidc:iss" = var.oidc_issuer, "oidc:aud" = "sts.aliyuncs.com", "oidc:sub" = "system:serviceaccount:raptor-system:agent-gateway" } } }] }
}
resource "alicloud_ram_role" "controller" {
  role_name                   = local.role_name
  description                 = "Singapore rdev Gateway sandbox key management and checkpoint reads"
  max_session_duration        = 3600
  assume_role_policy_document = var.rrsa_enabled ? jsonencode(local.rrsa_trust) : jsonencode(local.adoption_trust)
  depends_on                  = [terraform_data.guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_policy" "controller" {
  policy_name = local.role_name
  description = "Approved rdev sandbox Team key operations and private checkpoint reads"
  policy_document = jsonencode({ Version = "1", Statement = [
    { Effect = "Allow", Action = ["fcsandbox:CreateApiKey", "fcsandbox:ListApiKeys", "fcsandbox:UpdateApiKey", "fcsandbox:DeleteApiKey"], Resource = ["acs:fcsandbox:ap-southeast-1:${var.account_id}:teams/${var.team_id}", "acs:fcsandbox:ap-southeast-1:${var.account_id}:teams/${var.team_id}/apikeys/*"] },
    { Effect = "Allow", Action = ["fcsandbox:GetVolume"], Resource = ["acs:fcsandbox:ap-southeast-1:${var.account_id}:teams/${var.team_id}/volumes/${var.volume_id}"] },
    { Effect = "Allow", Action = ["oss:GetBucketEncryption"], Resource = ["acs:oss:*:${var.account_id}:${var.bucket}"] },
    { Effect = "Allow", Action = ["oss:GetObject"], Resource = ["acs:oss:*:${var.account_id}:${var.bucket}/auth/*"] }
  ] })
  depends_on = [terraform_data.guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_policy" "checkpoint" {
  policy_name     = "raptor-rdev-gateway-ax-checkpoint-write"
  description     = "AX portable checkpoint archive and checksum uploads only"
  policy_document = jsonencode({ Version = "1", Statement = [{ Effect = "Allow", Action = ["oss:PutObject"], Resource = ["acs:oss:*:${var.account_id}:${var.bucket}/auth/lifecycle/*.tgz", "acs:oss:*:${var.account_id}:${var.bucket}/auth/lifecycle/*.sha256"] }] })
  depends_on      = [terraform_data.guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_role_policy_attachment" "controller" {
  role_name   = alicloud_ram_role.controller.role_name
  policy_name = alicloud_ram_policy.controller.policy_name
  policy_type = "Custom"
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_role_policy_attachment" "checkpoint" {
  role_name   = alicloud_ram_role.controller.role_name
  policy_name = alicloud_ram_policy.checkpoint.policy_name
  policy_type = "Custom"
  lifecycle { prevent_destroy = true }
}
