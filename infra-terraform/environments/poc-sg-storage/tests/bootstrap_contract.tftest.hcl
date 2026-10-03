mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
  mock_resource "alicloud_vpc" { defaults = { id = "vpc-fixture" } }
  mock_resource "alicloud_vswitch" { defaults = { id = "vsw-fixture" } }
  mock_resource "alicloud_security_group" { defaults = { id = "sg-fixture" } }
  mock_resource "alicloud_ram_role" { defaults = { arn = "acs:ram::1234567890123456:role/raptor-storage" } }
}
variables { account_id = "1234567890123456" }
run "bootstrap_contract" {
  command = apply
  assert {
    condition     = output.vpc_id == "vpc-fixture" && output.vswitch_id == "vsw-fixture" && output.security_group_id == "sg-fixture" && output.execution_role_arn == "acs:ram::1234567890123456:role/raptor-storage" && output.nas_policy_attached == false
    error_message = "Root outputs must preserve the bootstrap interface and network-only stage."
  }
}
run "permission_stage" {
  command = apply
  variables {
    filesystem_id   = "fs-fixture"
    access_point_id = "ap-fixture"
  }
  assert {
    condition     = output.nas_policy_attached
    error_message = "Root must forward the paired storage IDs."
  }
}
