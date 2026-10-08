mock_provider "alicloud" {
  mock_data "alicloud_account" {
    defaults = { id = "1234567890123456" }
  }
}
variables {
  account_id          = "1234567890123456"
  env_code            = "fixture.env"
  proxy_code          = "proxy-fixture"
  target_db_code      = "db-fixture"
  target_db_env       = "fixture.env"
  target_db_host      = "private.example.invalid"
  target_database     = "test"
  region              = "ap-southeast-1"
  vpc_id              = "vpc-fixture"
  vswitch_ids         = ["vsw-fixture"]
  nlb_zones           = [{ zone_id = "ap-southeast-1a", vswitch_id = "vsw-a" }, { zone_id = "ap-southeast-1b", vswitch_id = "vsw-b" }]
  security_group_id   = "sg-fixture"
  image_id            = "image-fixture"
  instance_class      = "ecs.fixture"
  secret_reference    = "secret-fixture-reference"
  execution_role_name = "reviewed-fixture-role"
  bootstrap_revision  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  bootstrap_reviewed  = true
}
run "private_tagged_capacity" {
  command = plan
  assert {
    condition     = alicloud_ess_scaling_group.proxy.desired_capacity == 2 && alicloud_ess_scaling_group.proxy.max_size == 4 && alicloud_ess_scaling_group.proxy.min_size == 2
    error_message = "Bounded baseline/surge required."
  }
  assert {
    condition     = alicloud_ess_scaling_group.proxy.tags["env"] == "fixture.env" && alicloud_ess_scaling_group.proxy.tags["db-proxy-code"] == "proxy-fixture" && alicloud_ess_scaling_group.proxy.tags["target-db-code"] == "db-fixture"
    error_message = "Proxy association tags required."
  }
  assert {
    condition     = alicloud_ess_scaling_configuration.proxy.tags["db-proxy-code"] == "proxy-fixture" && alicloud_ess_scaling_configuration.proxy.internet_max_bandwidth_out == 0
    error_message = "Nodes must have association tags and no public bandwidth."
  }
  assert {
    condition     = alicloud_nlb_load_balancer.proxy.address_type == "Intranet" && alicloud_nlb_server_group.proxy.connection_drain_enabled == false && alicloud_ess_server_group_attachment.proxy.type == "NLB"
    error_message = "Private NLB and explicit ESS attachment required."
  }
}
run "cross_environment_target" {
  command = plan
  variables { target_db_env = "another.env" }
  expect_failures = [var.target_db_env]
}
run "unreviewed_bootstrap" {
  command = plan
  variables { bootstrap_reviewed = false }
  expect_failures = [var.bootstrap_reviewed]
}
run "wrong_region" {
  command = plan
  variables { region = "cn-hangzhou" }
  expect_failures = [var.region]
}
