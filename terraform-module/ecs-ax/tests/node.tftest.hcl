mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
  mock_data "alicloud_vswitches" { defaults = { vswitches = [{ id = "vsw-t4nop9qf6v46gw2sa8l7d", vpc_id = "vpc-t4n4fi6r1a7bi6n93ftq3", cidr_block = "10.70.1.0/24" }] } }
}
variables {
  ssh_public_key = "ssh-ed25519 AAAATEST"
  admin_cidr     = "192.0.2.1/32"
}
run "single_persistent_node" {
  command = plan
  assert {
    condition     = alicloud_instance.node.instance_type == "ecs.u2i-c1m8.large" && alicloud_instance.node.system_disk_encrypted && alicloud_instance.node.system_disk_size == 120
    error_message = "POC requires one persistent encrypted node."
  }
  assert {
    condition     = alicloud_security_group_rule.ssh.cidr_ip == "192.0.2.1/32" && alicloud_instance.node.tags["Environment"] == "rdev.ali"
    error_message = "SSH must be restricted and node belongs to rdev.ali."
  }
}
run "reject_ipv6_administrator_range" {
  command = plan
  variables { admin_cidr = "2001:db8::/32" }
  expect_failures = [var.admin_cidr]
}
run "reject_wrong_account" {
  command = plan
  override_data {
    target = data.alicloud_account.current
    values = { id = "9999999999999999" }
  }
  expect_failures = [terraform_data.guard]
}

run "component_owned_gitops_permissions" {
  command = plan
  variables {
    gitops_state_bucket = "raptor-iap-tfstate-sg-200743"
    gitops_plan_role    = "raptor-iap-rdev-plan"
    gitops_apply_role   = "raptor-iap-rdev-apply"
  }
  assert {
    condition = (
      length(alicloud_ram_policy.gitops) == 2 &&
      alicloud_ram_role_policy_attachment.gitops["apply"].role_name == "raptor-iap-rdev-apply" &&
      contains(flatten([for statement in jsondecode(alicloud_ram_policy.gitops["apply"].policy_document).Statement : statement.Action]), "ecs:ModifyInstanceSpec") &&
      !contains(flatten([for statement in jsondecode(alicloud_ram_policy.gitops["plan"].policy_document).Statement : statement.Action]), "ecs:ModifyInstanceSpec")
    )
    error_message = "AX component must own scoped GitOps access; the plan role remains read-only."
  }
}
