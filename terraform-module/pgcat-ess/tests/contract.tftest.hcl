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
  container_image     = "ghcr.io/example/pgcat@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  instance_class      = "ecs.fixture"
  secret_reference    = "oss://fixture-bucket/fixture/config.json?versionId=fixture-version"
  execution_role_name = "reviewed-fixture-role"
  bootstrap_revision  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  bootstrap_reviewed  = true
}
run "private_tagged_capacity" {
  command = plan
  assert {
    condition     = alicloud_ess_scaling_configuration.proxy.system_disk_encrypted == true && strcontains(base64decode(alicloud_ess_scaling_configuration.proxy.user_data), "docker run -d") && strcontains(base64decode(alicloud_ess_scaling_configuration.proxy.user_data), "--restart unless-stopped") && !strcontains(base64decode(alicloud_ess_scaling_configuration.proxy.user_data), "test -x /opt/raptor-pgcat/bootstrap")
    error_message = "Encrypted disk and Docker startup from a standard ECS image required."
  }
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

run "mutable_container_rejected" {
  command = plan
  variables { container_image = "ghcr.io/example/pgcat:latest" }
  expect_failures = [var.container_image]
}
run "component_owned_private_network" {
  command = plan
  variables {
    security_group_id    = null
    sql_client_cidrs     = ["10.70.1.0/24", "10.70.4.0/24"]
    metrics_client_cidrs = ["10.70.1.0/24"]
  }
  assert {
    condition     = length(alicloud_security_group.proxy) == 1 && length(alicloud_security_group_rule.sql) == 2 && length(alicloud_security_group_rule.metrics) == 1
    error_message = "PgCat must own its private SQL/metrics network rules."
  }
  assert {
    condition     = alltrue([for r in alicloud_security_group_rule.sql : r.port_range == "6432/6432"]) && alltrue([for r in alicloud_security_group_rule.metrics : r.port_range == "9930/9930"])
    error_message = "Do not widen service ports."
  }
}
run "reject_public_network" {
  command = plan
  variables { sql_client_cidrs = ["0.0.0.0/0"] }
  expect_failures = [var.sql_client_cidrs]
}
run "reject_public_nonzero_source" {
  command = plan
  variables { sql_client_cidrs = ["8.8.8.0/24"] }
  expect_failures = [var.sql_client_cidrs]
}
