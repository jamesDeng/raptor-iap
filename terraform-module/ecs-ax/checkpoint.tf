# Aliyun Sandbox persists through its mounted Volume. AX exports portable
# checkpoints through Gateway, requiring this additional, object-scoped write.
resource "alicloud_ram_policy" "ax_checkpoint_write" {
  policy_name = "raptor-rdev-gateway-ax-checkpoint-write"
  description = "AX portable checkpoint archive and checksum uploads only"
  policy_document = jsonencode({
    Version = "1"
    Statement = [{
      Effect = "Allow"
      Action = ["oss:PutObject"]
      Resource = [
        "acs:oss:*:1360282071200743:raptor-pi-auth-1360282071200743-20261004/auth/lifecycle/*.tgz",
        "acs:oss:*:1360282071200743:raptor-pi-auth-1360282071200743-20261004/auth/lifecycle/*.sha256"
      ]
    }]
  })
  depends_on = [terraform_data.guard]
}
resource "alicloud_ram_role_policy_attachment" "ax_checkpoint_write" {
  role_name   = "raptor-rdev-gateway-controller"
  policy_name = alicloud_ram_policy.ax_checkpoint_write.policy_name
  policy_type = "Custom"
}
