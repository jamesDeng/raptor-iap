mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
run "protected_state" {
  command = plan
  variables {
    account_id         = "1234567890123456"
    bucket_name        = "raptor-iap-test-state"
    lock_instance_name = "raptor-tf-lock"
  }
  assert {
    condition     = alicloud_ots_instance.lock.instance_type == "HighPerformance"
    error_message = "Singapore lock store must use supported HighPerformance type."
  }
  assert {
    condition     = alicloud_oss_bucket_acl.state.acl == "private" && alicloud_oss_bucket.state.force_destroy == false
    error_message = "State must be private and protected from nonempty deletion."
  }
  assert {
    condition     = alicloud_oss_bucket.state.server_side_encryption_rule[0].sse_algorithm == "AES256" && alicloud_oss_bucket.state.versioning[0].status == "Enabled"
    error_message = "State requires encryption and recoverable versions."
  }
  assert {
    condition     = alicloud_ots_table.lock.primary_key[0].name == "LockID" && alicloud_ots_table.lock.time_to_live == -1
    error_message = "Terraform locks must use LockID and not silently expire."
  }
}
