mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "123456789012" } }
}
variables {
  account_id    = "123456789012"
  vpc_id        = "vpc-fixture"
  db_vswitch_id = "vsw-fixture"
  config_bucket = "test-config"
  db_code       = "db-fixture"
}
run "encrypted_evidence_prefix_retention" {
  command = plan
  assert {
    condition     = length(alicloud_oss_bucket.config.lifecycle_rule) == 1 && try(alicloud_oss_bucket.config.lifecycle_rule[0].prefix == "traffic-evidence/" && alicloud_oss_bucket.config.lifecycle_rule[0].enabled && one(alicloud_oss_bucket.config.lifecycle_rule[0].expiration).days == 7 && one(alicloud_oss_bucket.config.lifecycle_rule[0].noncurrent_version_expiration).days == 1, false)
    error_message = "Only traffic evidence may expire: current after7days, noncurrent after1day."
  }
  assert {
    condition     = alicloud_oss_bucket.config.server_side_encryption_rule[0].sse_algorithm == "AES256" && alicloud_oss_bucket.config.versioning[0].status == "Enabled" && alicloud_oss_bucket_acl.config.acl == "private"
    error_message = "Evidence storage must preserve encrypted private versioned bucket."
  }
}
