mock_provider "alicloud" {
  mock_data "alicloud_account" {
    defaults = { id = "1234567890123456" }
  }
}
variables {
  account_id               = "1234567890123456"
  vpc_id                   = "vpc-fixture"
  nlb_zones                = [{ zone_id = "ap-southeast-1a", vswitch_id = "vsw-one" }, { zone_id = "ap-southeast-1b", vswitch_id = "vsw-two" }]
  endpoint_zone_id         = "ap-southeast-1a"
  worker_instance_id       = "i-fixture"
  worker_security_group_id = "sg-fixture"
  proxy_client_cidrs       = ["10.70.4.0/24"]
  nlb_backend_cidrs        = ["10.70.1.0/24", "10.70.4.0/24"]
}
run "private_write_only_receiver" {
  command = plan
  assert {
    condition = (
      alicloud_nlb_load_balancer.receiver.address_type == "Intranet" &&
      alicloud_nlb_load_balancer.receiver.deletion_protection_enabled &&
      alicloud_nlb_listener.receiver.listener_port == 9091 &&
      alicloud_nlb_server_group_server_attachment.worker.server_id == "i-fixture" &&
      alicloud_nlb_server_group_server_attachment.worker.port == 30909
    )
    error_message = "The receiver must be private, protected and bound to the fixed ACK worker NodePort."
  }
  assert {
    condition = (
      alicloud_security_group_rule.clients["10.70.4.0/24"].policy == "accept" &&
      alicloud_security_group_rule.deny_other_clients.policy == "drop" &&
      alicloud_security_group_rule.deny_other_clients.cidr_ip == "0.0.0.0/0" &&
      length(alicloud_security_group_rule.worker_nodeport) == 2
    )
    error_message = "Only reviewed PgCat subnets may enter the NLB and fixed private subnets may reach the worker."
  }
}
run "public_client_rejected" {
  command = plan
  variables { proxy_client_cidrs = ["0.0.0.0/0"] }
  expect_failures = [var.proxy_client_cidrs]
}
run "wrong_worker_group_rejected" {
  command = plan
  variables { worker_security_group_id = "other" }
  expect_failures = [var.worker_security_group_id]
}
