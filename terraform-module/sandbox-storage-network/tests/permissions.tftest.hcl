mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
variables { account_id = "1234567890123456" }
run "no_storage_policy" {
  command = plan
  assert {
    condition     = length(alicloud_ram_policy.storage) == 0 && length(alicloud_ram_role_policy_attachment.storage) == 0 && output.nas_policy_attached == false
    error_message = "Network-only configuration must grant no NAS data access."
  }
}
run "scoped_storage_policy" {
  command = plan
  variables {
    filesystem_id   = "fs-fixture"
    access_point_id = "ap-fixture"
  }
  assert {
    condition     = length(alicloud_ram_policy.storage) == 1 && length(alicloud_ram_role_policy_attachment.storage) == 1 && output.nas_policy_attached
    error_message = "Permission stage must attach exactly one custom policy."
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.storage[0].policy_document).Statement[0].Resource[0] == "acs:nas:ap-southeast-1:1234567890123456:filesystem/fs-fixture" && jsondecode(alicloud_ram_policy.storage[0].policy_document).Statement[0].Condition.StringEquals["nas:AccessPointArn"] == "acs:nas:ap-southeast-1:1234567890123456:accesspoint/ap-fixture"
    error_message = "Policy must bind the exact filesystem and Access Point."
  }
  assert {
    condition     = toset(jsondecode(alicloud_ram_policy.storage[0].policy_document).Statement[0].Action) == toset(["nas:ClientMount", "nas:ClientWrite", "nas:ClientRootAccess"]) && length(regexall("\\*", alicloud_ram_policy.storage[0].policy_document)) == 0 && alicloud_ram_policy.storage[0].force == false && alicloud_ram_role_policy_attachment.storage[0].policy_type == "Custom"
    error_message = "Unexpected action, wildcard or force deletion."
  }
}
run "partial_ids_rejected" {
  command = plan
  variables { filesystem_id = "fs-fixture" }
  expect_failures = [var.access_point_id]
}
run "malformed_ids_rejected" {
  command = plan
  variables {
    filesystem_id   = "fs-fixture"
    access_point_id = "acs:nas:*"
  }
  expect_failures = [var.access_point_id]
}
run "permission_stage_wrong_account" {
  command = plan
  variables {
    account_id      = "9999999999999999"
    filesystem_id   = "fs-fixture"
    access_point_id = "ap-fixture"
  }
  expect_failures = [terraform_data.account_guard]
}
