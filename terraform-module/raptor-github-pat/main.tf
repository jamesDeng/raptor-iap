data "alicloud_account" "current" {}

resource "terraform_data" "guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Raptor account boundary mismatch."
    }
    precondition {
      condition     = var.oidc_provider_arn == "acs:ram::${var.account_id}:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1" && startswith(var.oidc_issuer, "https://")
      error_message = "Verified ACK RRSA issuer/provider required."
    }
  }
}

resource "alicloud_oss_bucket" "pat" {
  bucket = var.bucket
  server_side_encryption_rule { sse_algorithm = "AES256" }
  force_destroy = false
  lifecycle { prevent_destroy = true }
  depends_on = [terraform_data.guard]
}
resource "alicloud_oss_bucket_acl" "pat" {
  bucket = alicloud_oss_bucket.pat.bucket
  acl    = "private"
  lifecycle { prevent_destroy = true }
}

resource "alicloud_ram_role" "backend" {
  role_name                   = "raptor-rdev-github-pat-backend"
  description                 = "Raptor backend GitHub PAT object storage"
  max_session_duration        = 3600
  assume_role_policy_document = jsonencode({ Version = "1", Statement = [{ Effect = "Allow", Action = "sts:AssumeRole", Principal = { Federated = [var.oidc_provider_arn] }, Condition = { StringEquals = { "oidc:iss" = var.oidc_issuer, "oidc:aud" = "sts.aliyuncs.com", "oidc:sub" = "system:serviceaccount:raptor-system:raptor-backend" } } }] })
  depends_on                  = [terraform_data.guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_policy" "backend" {
  policy_name = "raptor-rdev-github-pat-backend"
  description = "Read and update only dedicated GitHub PAT objects"
  policy_document = jsonencode({ Version = "1", Statement = [
    { Effect = "Allow", Action = ["oss:PutObject", "oss:GetObject", "oss:DeleteObject"], Resource = ["acs:oss:*:${var.account_id}:${var.bucket}/github-pat/*"] }
  ] })
  depends_on = [terraform_data.guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_role_policy_attachment" "backend" {
  role_name   = alicloud_ram_role.backend.role_name
  policy_name = alicloud_ram_policy.backend.policy_name
  policy_type = "Custom"
  lifecycle { prevent_destroy = true }
}
