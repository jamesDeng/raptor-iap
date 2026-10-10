mock_provider "alicloud" {}
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
