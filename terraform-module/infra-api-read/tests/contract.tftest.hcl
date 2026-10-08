mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
variables {
  gateway_instance_id = "shared-fixture"
  account_id          = "1234567890123456"
  name                = "raptor-read-test"
  function_name       = "raptor-read-test"
  trigger_url         = "https://fixture.ap-southeast-1.fcapp.run"
}
run "read_only_contract" {
  command = plan
  assert {
    condition     = alicloud_api_gateway_api.read["identity"].fc_service_config[0].function_version == "3.0"
    error_message = "Backend must be FC3 HTTP."
  }
  assert {
    condition     = length([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s if contains(s.Action, "*")]) == 0
    error_message = "No wildcard action."
  }
  assert {
    condition     = alltrue([for api in alicloud_api_gateway_api.read : api.request_config[0].mode == "PASSTHROUGH" && length(api.request_parameters) == 0])
    error_message = "Pass-through routes must not declare mapped request parameters; the service authenticates the forwarded caller header."
  }
}

run "wrong_account" {
  command = plan
  variables { account_id = "9999999999999999" }
  expect_failures = [terraform_data.account_guard]
}

run "ack_only_contract" {
  command = plan
  variables {
    ack_only   = true
    cluster_id = "owned-cluster"
  }
  assert {
    condition     = toset(flatten([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s.Action])) == toset(["sts:GetCallerIdentity", "cs:DescribeClusterUserKubeconfig"])
    error_message = "ACK-only role must not acquire database, ESS, wildcard or mutation permissions."
  }
  assert {
    condition     = contains(jsondecode(alicloud_ram_policy.read.policy_document).Statement[1].Resource, "acs:cs:ap-southeast-1:1234567890123456:cluster/owned-cluster")
    error_message = "Kubeconfig access must be restricted to the selected cluster."
  }
}

run "ack_with_rds_discovery_contract" {
  command = plan
  variables {
    ack_only             = true
    cluster_id           = "owned-cluster"
    enable_rds_discovery = true
  }
  assert {
    condition = toset(flatten([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s.Action])) == toset([
      "sts:GetCallerIdentity", "cs:DescribeClusterUserKubeconfig", "rds:DescribeDBInstances", "rds:DescribeTags"
    ])
    error_message = "RDS discovery must add only the two read actions, without ESS or mutation access."
  }
}
