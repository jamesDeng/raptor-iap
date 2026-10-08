mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
variables {
  account_id = "1234567890123456"
  vpc_id     = "vpc-fixture"
  vswitch_id = "vsw-fixture"
}
run "private_builder" {
  command = plan
  assert {
    condition     = alicloud_instance.builder.internet_max_bandwidth_out == 0 && alicloud_instance.builder.system_disk_encrypted && alicloud_instance.builder.system_disk_size == 20 && alicloud_instance.builder.instance_charge_type == "PostPaid"
    error_message = "Builder must be private, encrypted and bounded in size."
  }
}
run "wrong_account" {
  command = plan
  variables { account_id = "9999999999999999" }
  expect_failures = [alicloud_security_group.builder]
}
