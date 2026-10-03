mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
variables { account_id = "1234567890123456" }
run "saved_plan" { command = plan }

run "changed_identity_before_execution" {
  command = apply
  override_data {
    target = data.alicloud_account.at_apply
    values = { id = "9999999999999999" }
  }
  expect_failures = [terraform_data.account_guard]
}
