mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
variables {
  account_id      = "1234567890123456"
  name            = "restart-fixture"
  group_id        = "existing-group"
  function_name   = "existing-function"
  trigger_url     = "https://fixture.ap-southeast-1.fcapp.run/"
  invoke_role_arn = "acs:ram::1234567890123456:role/existing-invoke"
}
run "restart_route_contract" {
  command = plan
  assert {
    condition     = alicloud_api_gateway_api.restart.request_config[0].method == "POST" && alicloud_api_gateway_api.restart.request_config[0].path == "/v1/deployment-restart"
    error_message = "Only the reviewed restart POST route is allowed."
  }
  assert {
    condition     = alicloud_api_gateway_api.restart.group_id == "existing-group" && alicloud_api_gateway_api.restart.fc_service_config[0].function_name == "existing-function"
    error_message = "Must reuse the supplied existing group/function."
  }
  assert {
    condition     = alicloud_api_gateway_api.restart.fc_service_config[0].content_type_category == "CLIENT" && alicloud_api_gateway_api.restart.fc_service_config[0].function_base_url == "https://fixture.ap-southeast-1.fcapp.run/"
    error_message = "Preserve caller JSON content type and exact trigger URL."
  }
  assert {
    condition     = alicloud_api_gateway_api.restart.request_config[0].protocol == "HTTPS" && alicloud_api_gateway_api.restart.request_config[0].mode == "PASSTHROUGH" && alicloud_api_gateway_api.restart.fc_service_config[0].arn_role == var.invoke_role_arn
    error_message = "Use HTTPS pass-through and the reviewed existing invocation role."
  }
}
run "wrong_account" {
  command = plan
  variables { account_id = "9999999999999999" }
  expect_failures = [terraform_data.account_guard, var.invoke_role_arn]
}
run "invalid_origin" {
  command = plan
  variables { trigger_url = "http://fixture.invalid/" }
  expect_failures = [var.trigger_url]
}
