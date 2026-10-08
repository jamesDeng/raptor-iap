mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
variables {
  account_id  = "1234567890123456"
  role_name   = "fixture-pgcat"
  bucket_name = "fixture-bucket"
  object_key  = "rdev.ali/pgcat/config.json"
}
run "exact_object_role" {
  command = plan
  assert {
    condition     = toset(jsondecode(alicloud_ram_policy.config.policy_document).Statement[0].Action) == toset(["oss:GetObject", "oss:GetObjectVersion"]) && jsondecode(alicloud_ram_policy.config.policy_document).Statement[0].Resource == ["acs:oss:*:1234567890123456:fixture-bucket/rdev.ali/pgcat/config.json"]
    error_message = "Only the exact versioned config object may be read."
  }
  assert {
    condition     = jsondecode(alicloud_ram_role.config.assume_role_policy_document).Statement[0].Principal.Service == ["ecs.aliyuncs.com"]
    error_message = "Only ECS may assume PgCat role."
  }
}
run "reject_broad_object_policy" {
  command = plan
  variables { object_key = "rdev.ali/*" }
  expect_failures = [var.object_key]
}
