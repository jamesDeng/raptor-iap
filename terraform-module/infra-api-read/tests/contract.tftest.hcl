mock_provider "alicloud" {
 mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
variables {
  account_id = "1234567890123456"
  name = "raptor-read-test"
  function_name = "raptor-read-test"
  trigger_url = "https://fixture.ap-southeast-1.fcapp.run"
}
run "read_only_contract" {
  command = plan
  assert {
    condition = alicloud_api_gateway_api.read["identity"].fc_service_config[0].function_version == "3.0"
    error_message = "Backend must be FC3 HTTP."
  }
  assert {
    condition = length([for s in jsondecode(alicloud_ram_policy.read.policy_document).Statement : s if contains(s.Action, "*")]) == 0
    error_message = "No wildcard action."
  }
  assert {
    condition = one(alicloud_api_gateway_api.read["identity"].request_parameters).name == "X-Infra-Authorization"
    error_message = "Dedicated caller header required."
  }
}

run "wrong_account" {
 command = plan
 variables { account_id = "9999999999999999" }
 expect_failures = [terraform_data.account_guard]
}
