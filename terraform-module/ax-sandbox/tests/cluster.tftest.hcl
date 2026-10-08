mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
  mock_data "alicloud_vpcs" {
    defaults = { vpcs = [{ id = "vpc-t4n4fi6r1a7bi6n93ftq3", cidr_block = "10.70.0.0/16", tags = { Project = "raptor-iap", Environment = "rdev.ali", Owner = "rdev-foundation" } }] }
  }
}
variables {
  account_id         = "1360282071200743"
  vpc_id             = "vpc-t4n4fi6r1a7bi6n93ftq3"
  kubernetes_version = "1.36.2-aliyun.1"
}
run "private_minimal_cluster" {
  command = plan
  assert {
    condition     = length(alicloud_eip_address.api) == 0 && alicloud_cs_managed_kubernetes.cluster.new_nat_gateway == false
    error_message = "Compatibility cluster must stay private and reuse outbound infrastructure."
  }
  assert {
    condition     = alicloud_cs_managed_kubernetes.cluster.version == "1.36.2-aliyun.1" && alicloud_cs_managed_kubernetes.cluster.skip_set_certificate_authority
    error_message = "Version must be pinned and kubeconfig excluded."
  }
}
run "rdev_environment_tags" {
  command = plan
  assert {
    condition     = alicloud_cs_managed_kubernetes.cluster.tags["Environment"] == "rdev.ali" && alicloud_vswitch.workers.tags["Environment"] == "rdev.ali"
    error_message = "Sandbox cluster belongs to the existing rdev.ali environment."
  }
}
run "wrong_account_rejected" {
  command = plan
  variables { account_id = "9999999999999999" }
  expect_failures = [terraform_data.account_guard]
}

run "authorized_public_api" {
  command = plan
  variables {
    public_api_enabled = true
    api_load_balancer_id = "lb-test"
  }
  assert {
    condition = length(alicloud_eip_address.api) == 1 && alicloud_eip_association.api[0].instance_id == "lb-test" && alicloud_eip_association.api[0].instance_type == "SlbInstance"
    error_message = "Explicit public API opt-in must enable the endpoint."
  }
}
