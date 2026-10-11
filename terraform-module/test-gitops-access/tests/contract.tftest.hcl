mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "123456789012" } }
}
variables {
  account_id      = "123456789012"
  state_bucket    = "test-state"
  config_bucket   = "test-config"
  plan_role_name  = "test-plan"
  apply_role_name = "test-apply"
}
run "state_scope" {
  command = plan
  assert {
    condition     = jsondecode(alicloud_ram_policy.state["plan"].policy_document).Statement[1].Action == ["oss:GetObject"]
    error_message = "Plan identity must not write state."
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.state["apply"].policy_document).Statement[1].Resource == ["acs:oss:*:123456789012:test-state/rdev.ali/test-pgcat/terraform.tfstate"]
    error_message = "Apply access must bind the exact test-state object."
  }
  assert {
    condition     = jsondecode(alicloud_ram_policy.state["plan"].policy_document).Statement[2].Resource == ["acs:oss:*:123456789012:test-config"]
    error_message = "Config metadata access must not cover unrelated buckets."
  }
}

run "apply_only_exact_bucket_retention" {
  command = plan
  assert {
    condition     = length([for s in jsondecode(alicloud_ram_policy.state["plan"].policy_document).Statement : s if contains(s.Action, "oss:PutBucketLifecycle")]) == 0
    error_message = "Plan identity must not alter retention."
  }
  assert {
    condition     = length([for s in jsondecode(alicloud_ram_policy.state["apply"].policy_document).Statement : s if contains(s.Action, "oss:PutBucketLifecycle")]) == 1 && try(one([for s in jsondecode(alicloud_ram_policy.state["apply"].policy_document).Statement : s if contains(s.Action, "oss:PutBucketLifecycle")]).Resource == ["acs:oss:*:123456789012:test-config"], false)
    error_message = "Retention write must bind only apply identity and exact config bucket."
  }
}

run "catalog_tag_migration_exact_resources" {
  command = plan
  variables {
    catalog_untag_targets = {
      scaling_group_id  = "asg-test"
      security_group_id = "sg-test"
      load_balancer_id  = "nlb-test"
      server_group_id   = "sgp-test"
      db_instance_id    = "pgm-test"
    }
  }
  assert {
    condition = jsondecode(alicloud_ram_policy.catalog_untag[0].policy_document).Statement == [
      { Effect = "Allow", Action = ["ess:UntagResources"], Resource = ["acs:ess:ap-southeast-1:123456789012:scalinggroup/asg-test"] },
      { Effect = "Allow", Action = ["ecs:UntagResources"], Resource = ["acs:ecs:ap-southeast-1:123456789012:securitygroup/sg-test"] },
      { Effect = "Allow", Action = ["nlb:UntagResources"], Resource = ["acs:nlb:ap-southeast-1:123456789012:loadbalancer/nlb-test", "acs:nlb:ap-southeast-1:123456789012:servergroup/sgp-test"] },
      { Effect = "Allow", Action = ["rds:UnTagResources"], Resource = ["acs:rds:ap-southeast-1:123456789012:dbinstance/pgm-test"] }
    ]
    error_message = "Untag permissions must bind only the five existing Singapore POC resources."
  }
  assert {
    condition     = alicloud_ram_role_policy_attachment.catalog_untag[0].role_name == "test-apply"
    error_message = "The read-only plan identity must never receive untag permissions."
  }
}
run "catalog_untag_absent_by_default" {
  command = plan
  assert {
    condition     = length(alicloud_ram_policy.catalog_untag) == 0 && length(alicloud_ram_role_policy_attachment.catalog_untag) == 0
    error_message = "No catalog migration grant should exist without explicit target configuration."
  }
}
