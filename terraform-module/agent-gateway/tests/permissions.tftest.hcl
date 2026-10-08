mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
}
variables {
  team_id   = "db28b89e-6cbd-53f8-a4a3-c33600d52aa7"
  volume_id = "34381ac2-c693-45d9-9a24-9ddbf7b91da2"
  bucket    = "raptor-pi-auth-1360282071200743-20261004"
}
run "adoption_preserves_identity_and_permissions" {
  command = plan
  assert {
    condition     = alicloud_ram_role.controller.role_name == "raptor-rdev-gateway-controller" && alicloud_ram_role.controller.max_session_duration == 3600 && length(jsondecode(alicloud_ram_policy.controller.policy_document).Statement) == 4
    error_message = "Adopt the original controller identity and permission statements."
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.checkpoint.policy_document).Statement[0].Action == ["oss:PutObject"] && toset(jsondecode(alicloud_ram_policy.checkpoint.policy_document).Statement[0].Resource) == toset(["acs:oss:*:1360282071200743:raptor-pi-auth-1360282071200743-20261004/auth/lifecycle/*.tgz", "acs:oss:*:1360282071200743:raptor-pi-auth-1360282071200743-20261004/auth/lifecycle/*.sha256"]) && alicloud_ram_role_policy_attachment.checkpoint.policy_type == "Custom"
    error_message = "Checkpoint writes must remain exact archive/checksum object scope."
  }
}
run "rrsa_exact_trust" {
  command = plan
  variables {
    rrsa_enabled      = true
    oidc_provider_arn = "acs:ram::1360282071200743:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1"
    oidc_issuer       = "https://issuer.example/c92787e953503492ea141a744c81498f1"
  }
  assert {
    condition     = length(jsondecode(alicloud_ram_role.controller.assume_role_policy_document).Statement) == 1 && jsondecode(alicloud_ram_role.controller.assume_role_policy_document).Statement[0].Action == "sts:AssumeRole" && jsondecode(alicloud_ram_role.controller.assume_role_policy_document).Statement[0].Condition.StringEquals["oidc:sub"] == "system:serviceaccount:raptor-system:agent-gateway" && jsondecode(alicloud_ram_role.controller.assume_role_policy_document).Statement[0].Condition.StringEquals["oidc:aud"] == "sts.aliyuncs.com" && jsondecode(alicloud_ram_role.controller.assume_role_policy_document).Statement[0].Principal.Federated == ["acs:ram::1360282071200743:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1"]
    error_message = "RRSA must restrict principal, namespace, SA and audience with no bootstrap trust."
  }
}
run "reject_wrong_account" {
  command = plan
  override_data {
    target = data.alicloud_account.current
    values = { id = "9999999999999999" }
  }
  expect_failures = [terraform_data.guard]
}
