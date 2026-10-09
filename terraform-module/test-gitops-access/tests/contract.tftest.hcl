mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "123456789012" } }
}
variables {
  account_id      = "123456789012"
  state_bucket    = "test-state"
  config_bucket   = "test-config"
  plan_role_name  = "test-plan"
  apply_role_name = "test-apply"
}
run "state_scope" {
  command = plan
  assert {
    condition     = jsondecode(alicloud_ram_policy.state["plan"].policy_document).Statement[1].Action == ["oss:GetObject"]
    error_message = "Plan identity must not write state."
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.state["apply"].policy_document).Statement[1].Resource == ["acs:oss:*:123456789012:test-state/rdev.ali/test-pgcat/terraform.tfstate"]
    error_message = "Apply access must bind the exact test-state object."
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.state["plan"].policy_document).Statement[2].Resource == ["acs:oss:*:123456789012:test-config"]
    error_message = "Config metadata access must not cover unrelated buckets."
  }
}
