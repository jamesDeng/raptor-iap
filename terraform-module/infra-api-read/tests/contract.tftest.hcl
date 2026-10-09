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

run "proxy_permissions_and_routes" {
  command = plan
  variables {
    ack_only              = true
    cluster_id            = "cluster"
    enable_proxy_commands = true
    proxy_targets = {
      proxy = { group_id = "asg-test", server_group_id = "sgp-test", load_balancer_id = "nlb-test" }
    }
  }
  assert {
    condition     = alltrue([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s.Resource == ["acs:ess:ap-southeast-1:1234567890123456:scalinggroup/asg-test"] if contains(s.Action, "ess:ModifyScalingGroup") || contains(s.Action, "ess:SetInstancesProtection")])
    error_message = "ESS mutation must be pinned to the POC group."
  }
  assert {
    condition     = length([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s if contains(s.Action, "ess:ModifyScalingGroup")]) == 1 && length([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s if contains(s.Action, "nlb:RemoveServersFromServerGroup")]) == 1
    error_message = "Required runtime capabilities must exist exactly once."
  }
  assert {
    condition     = toset(keys(alicloud_api_gateway_api.proxy)) == toset(["scale", "protection", "deregistration"])
    error_message = "Expose exactly the reviewed individual commands."
  }
  assert {
    condition     = length([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s if contains(s.Action, "ecs:DeleteInstance") || contains(s.Action, "ess:RemoveInstances")]) == 0
    error_message = "Never grant direct node termination."
  }
}
run "infra_gitops_permission_owner" {
  command = plan
  override_resource {
    target          = alicloud_api_gateway_group.read
    override_during = plan
    values          = { id = "group-fixture" }
  }
  variables {
    gitops_plan_role    = "raptor-iap-rdev-plan"
    gitops_apply_role   = "raptor-iap-rdev-apply"
    gitops_state_bucket = "fixture-state"
  }
  assert {
    condition     = toset(keys(alicloud_ram_policy.gitops)) == toset(["plan", "apply"])
    error_message = "Deployment IAM must belong to this component."
  }
  assert {
    condition     = length([for s in jsondecode(alicloud_ram_policy.gitops["plan"].policy_document).Statement : s if contains(s.Action, "ram:CreatePolicyVersion") || contains(s.Action, "apigateway:CreateApi") || contains(s.Action, "oss:PutObject")]) == 0
    error_message = "Plan must not acquire cloud mutation or state-write rights."
  }
}
