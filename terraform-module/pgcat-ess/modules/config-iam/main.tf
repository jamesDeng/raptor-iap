data "alicloud_account" "current" {}
resource "alicloud_ram_role" "config" {
  role_name            = var.role_name
  description          = "Private rdev PgCat POC configuration object reader"
  max_session_duration = 3600

  assume_role_policy_document = jsonencode({
    Version   = "1"
    Statement = [{ Effect = "Allow", Action = "sts:AssumeRole", Principal = { Service = ["ecs.aliyuncs.com"] } }]
  })
  timeouts {}
  lifecycle {
    prevent_destroy = true
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Wrong Alibaba Cloud account for PgCat IAM."
    }
  }
}
resource "alicloud_ram_policy" "config" {
  policy_name = "${var.role_name}-read"
  description = "Read only one versioned rdev PgCat configuration object"

  policy_document = jsonencode({
    Version = "1"
    Statement = [{
      Effect   = "Allow"
      Action   = ["oss:GetObject", "oss:GetObjectVersion"]
      Resource = ["acs:oss:*:${var.account_id}:${var.bucket_name}/${var.object_key}"]
    }]
  })
  timeouts {}
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_role_policy_attachment" "config" {
  role_name   = alicloud_ram_role.config.role_name
  policy_name = alicloud_ram_policy.config.policy_name
  policy_type = "Custom"
  timeouts {}
  lifecycle { prevent_destroy = true }
}
