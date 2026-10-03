mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
  mock_resource "alicloud_vpc" { defaults = { id = "vpc-fixture" } }
  mock_resource "alicloud_vswitch" { defaults = { id = "vsw-fixture" } }
  mock_resource "alicloud_security_group" { defaults = { id = "sg-fixture" } }
  mock_resource "alicloud_ram_role" { defaults = { arn = "acs:ram::1234567890123456:role/raptor-storage" } }
}
variables { account_id = "1234567890123456" }
run "network_only" {
  command = apply
  assert {
    condition     = output.vpc_id == "vpc-fixture" && output.vswitch_id == "vsw-fixture" && output.security_group_id == "sg-fixture" && output.execution_role_arn == "acs:ram::1234567890123456:role/raptor-storage"
    error_message = "Bootstrap outputs must retain the resource IDs and role ARN."
  }
  assert {
    condition     = alicloud_vpc.network.cidr_block == "10.60.0.0/16" && alicloud_vswitch.sandbox.cidr_block == "10.60.1.0/24" && alicloud_security_group.sandbox.inner_access_policy == "Drop"
    error_message = "Network scope or internal isolation changed."
  }
  assert {
    condition     = alltrue([for rule in alicloud_security_group_rule.rules : rule.type != "ingress" || rule.policy == "drop"]) && length(alicloud_security_group_rule.rules) == 6
    error_message = "Unexpected inbound allow or missing explicit baseline/egress rules."
  }
  assert {
    condition     = jsondecode(alicloud_ram_role.execution.assume_role_policy_document).Statement[0].Principal.Service[0] == "fc.aliyuncs.com" && jsondecode(alicloud_ram_role.execution.assume_role_policy_document).Statement[0].Action[0] == "sts:AssumeRole" && alicloud_ram_role.execution.force == false
    error_message = "Execution trust or force-delete setting changed."
  }
}
run "wrong_account" {
  command = plan
  variables { account_id = "9999999999999999" }
  expect_failures = [terraform_data.account_guard]
}
run "wrong_region" {
  command = plan
  variables { region = "cn-hangzhou" }
  expect_failures = [var.region]
}
run "wrong_zone" {
  command = plan
  variables { zone = "ap-southeast-1b" }
  expect_failures = [var.zone]
}
run "invalid_cidrs" {
  command = plan
  variables { vpc_cidr = "bogus" }
  expect_failures = [var.vpc_cidr]
}
run "outside_vpc" {
  command = plan
  variables { vswitch_cidr = "10.61.1.0/24" }
  expect_failures = [var.vswitch_cidr]
}
run "noncanonical_cidr" {
  command = plan
  variables { vpc_cidr = "10.60.1.0/16" }
  expect_failures = [var.vpc_cidr]
}
run "ipv6_rejected" {
  command = plan
  variables { vpc_cidr = "2001:db8::/16" }
  expect_failures = [var.vpc_cidr]
}
run "wrong_mask" {
  command = plan
  variables { vswitch_cidr = "10.60.1.0/25" }
  expect_failures = [var.vswitch_cidr]
}
