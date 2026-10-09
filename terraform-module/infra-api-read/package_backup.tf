# Existing scoped deployment user; do not create or replace its credentials.
variable "deployment_user" {
  type    = string
  default = ""
}
resource "alicloud_ram_policy" "package_backup" {
  count       = var.deployment_user == "" ? 0 : 1
  depends_on  = [terraform_data.account_guard]
  policy_name = "${var.name}-package-backup"
  force       = false
  policy_document = jsonencode({ Version = "1", Statement = [{
    Effect   = "Allow"
    Action   = ["fc:GetFunctionCode"]
    Resource = ["acs:fc:${var.region}:${var.account_id}:functions/${var.function_name}"]
  }] })
}
resource "alicloud_ram_user_policy_attachment" "package_backup" {
  count       = var.deployment_user == "" ? 0 : 1
  user_name   = var.deployment_user
  policy_name = alicloud_ram_policy.package_backup[0].policy_name
  policy_type = "Custom"
}
