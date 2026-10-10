mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
}
run "dedicated_public_proxy" {
  command = plan
  variables {
    vswitch_id = "vsw-t4nop9qf6v46gw2sa8l7d"
  }
  assert {
    condition     = alicloud_slb_load_balancer.proxy.address_type == "internet" && alicloud_slb_load_balancer.proxy.instance_charge_type == "PayByCLCU"
    error_message = "Dedicated public usage-billed CLB required."
  }
  assert {
    condition     = length(alicloud_alidns_record.public) == 0
    error_message = "DNS publication must default off until HTTPS authentication passes."
  }
}

run "published_admin_dns" {
  command = plan
  variables {
    vswitch_id  = "vsw-t4nop9qf6v46gw2sa8l7d"
    publish_dns = true
  }
  assert {
    condition     = toset(keys(alicloud_alidns_record.public)) == toset(["raptor.rdev", "api.rdev", "admin.rdev"])
    error_message = "Public DNS must include the Admin hostname alongside existing hosts."
  }
}

run "planning_access_is_state_read_only" {
  command = plan
  variables {
    vswitch_id         = "vsw-t4nop9qf6v46gw2sa8l7d"
    enable_plan_access = true
    account_id         = "1360282071200743"
    state_bucket       = "raptor-iap-tfstate-sg-200743"
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.plan_access[0].policy_document).Statement[1].Resource == ["acs:oss:*:1360282071200743:raptor-iap-tfstate-sg-200743/rdev.ali/terraform.tfstate"]
    error_message = "Kong plan state read must bind one exact state object."
  }
  assert {
    condition     = !contains(flatten([for statement in jsondecode(alicloud_ram_policy.plan_access[0].policy_document).Statement : statement.Action]), "oss:PutObject")
    error_message = "PR planning identity must not write Terraform state."
  }
}
