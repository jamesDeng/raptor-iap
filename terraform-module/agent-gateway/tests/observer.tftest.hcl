mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
}
variables {
  team_id           = "db28b89e-6cbd-53f8-a4a3-c33600d52aa7"
  volume_id         = "34381ac2-c693-45d9-9a24-9ddbf7b91da2"
  bucket            = "raptor-pi-auth-1360282071200743-20261004"
  observer_enabled  = true
  observer_group_id = "asg-t4necb4dfmu6o3z2rn0g"
}
run "observer_is_fleet_read_only_and_trusts_only_gateway" {
  command = plan
  assert {
    condition     = jsondecode(alicloud_ram_policy.observer[0].policy_document).Statement[0].Action == ["ess:DescribeScalingInstances", "ess:DescribeScalingGroups"] && jsondecode(alicloud_ram_policy.observer[0].policy_document).Statement[0].Resource == ["acs:ess:ap-southeast-1:1360282071200743:scalinggroup/asg-t4necb4dfmu6o3z2rn0g"]
    error_message = "Observer must have only reads on the dedicated fleet."
  }
  assert {
    condition     = jsondecode(alicloud_ram_role.observer[0].assume_role_policy_document).Statement[0].Principal.RAM == ["acs:ram::1360282071200743:role/raptor-rdev-gateway-controller"]
    error_message = "Observer must trust only Gateway controller, never account root or sandbox."
  }
}
run "backend_reads_are_exact_nlb_resources" {
  command = plan
  variables {
    observer_backend_group_id = "sgp-test"
    observer_load_balancer_id = "nlb-test"
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.observer[0].policy_document).Statement[1].Action == ["nlb:ListServerGroupServers"] && jsondecode(alicloud_ram_policy.observer[0].policy_document).Statement[1].Resource == ["acs:nlb:ap-southeast-1:1360282071200743:serverGroup/sgp-test"] && jsondecode(alicloud_ram_policy.observer[0].policy_document).Statement[2].Resource == ["acs:nlb:ap-southeast-1:1360282071200743:loadbalancer/nlb-test"]
    error_message = "Backend reads must use exact provider resource types and IDs."
  }
}
run "sql_secret_read_is_controller_only_and_exact" {
  command = plan
  variables {
    observer_sql_bucket = "raptor-observer-private"
    observer_sql_key    = "rdev/sql-probe.json"
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.observer_assume[0].policy_document).Statement[2].Action == ["oss:GetObject"] && jsondecode(alicloud_ram_policy.observer_assume[0].policy_document).Statement[2].Resource == ["acs:oss:*:1360282071200743:raptor-observer-private/rdev/sql-probe.json"] && length(jsondecode(alicloud_ram_policy.observer[0].policy_document).Statement) == 1
    error_message = "SQL credentials are fetched only by Gateway from one exact encrypted object, never by observer role."
  }
}
