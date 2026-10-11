mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
}
variables {
  rrsa_enabled      = true
  oidc_provider_arn = "acs:ram::1360282071200743:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1"
  oidc_issuer       = "https://oidc-ack-ap-southeast-1.oss-ap-southeast-1.aliyuncs.com/c92787e953503492ea141a744c81498f1"
}
run "dedicated_rdev_observer_is_provisioned" {
  command = plan
  assert {
    condition     = module.gateway.observer_role_arn == "acs:ram::1360282071200743:role/raptor-rdev-agent-observer"
    error_message = "The rdev deployment must provision its scoped observer identity."
  }
}
run "explicit_null_preserves_initial_question_only_plan" {
  command = plan
  variables { observer = null }
  assert {
    condition     = module.gateway.observer_role_arn == null
    error_message = "Explicit null must retain the disabled observer path."
  }
}
