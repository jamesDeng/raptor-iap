data "alicloud_account" "current" {}

resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Receiver account mismatch."
    }
  }
}

locals {
  tags = {
    Project       = "raptor-iap"
    Environment   = "rdev.ali"
    Owner         = "infra-test-metrics"
    db-proxy-code = "DP00001"
  }
}

# This component owns its private endpoint. The NLB service-linked role is a
# documented shared prerequisite, already owned by the PgCat component state.
resource "alicloud_security_group" "receiver" {
  security_group_name = "raptor-rdev-pgcat-metrics-write"
  vpc_id              = var.vpc_id
  tags                = local.tags
  depends_on          = [terraform_data.account_guard]
}

resource "alicloud_security_group_rule" "clients" {
  for_each          = var.proxy_client_cidrs
  type              = "ingress"
  ip_protocol       = "tcp"
  nic_type          = "intranet"
  policy            = "accept"
  port_range        = "9091/9091"
  priority          = 2
  security_group_id = alicloud_security_group.receiver.id
  cidr_ip           = each.value
}

resource "alicloud_security_group_rule" "deny_other_clients" {
  type              = "ingress"
  ip_protocol       = "tcp"
  nic_type          = "intranet"
  policy            = "drop"
  port_range        = "9091/9091"
  priority          = 3
  security_group_id = alicloud_security_group.receiver.id
  cidr_ip           = "0.0.0.0/0"
}

resource "alicloud_nlb_load_balancer" "receiver" {
  load_balancer_name          = "raptor-rdev-pgcat-metrics-write"
  address_type                = "Intranet"
  address_ip_version          = "Ipv4"
  load_balancer_type          = "Network"
  payment_type                = "PayAsYouGo"
  vpc_id                      = var.vpc_id
  security_group_ids          = [alicloud_security_group.receiver.id]
  deletion_protection_enabled = true
  tags                        = local.tags
  dynamic "zone_mappings" {
    for_each = var.nlb_zones
    content {
      zone_id    = zone_mappings.value.zone_id
      vswitch_id = zone_mappings.value.vswitch_id
    }
  }
  depends_on = [alicloud_security_group_rule.clients, alicloud_security_group_rule.deny_other_clients]
}

resource "alicloud_nlb_server_group" "receiver" {
  server_group_name = "raptor-rdev-pgcat-metrics-write"
  server_group_type = "Instance"
  vpc_id            = var.vpc_id
  protocol          = "TCP"
  tags              = local.tags
  health_check {
    health_check_enabled      = true
    health_check_type         = "TCP"
    health_check_connect_port = 30909
  }
  depends_on = [terraform_data.account_guard]
}

resource "alicloud_security_group_rule" "worker_nodeport" {
  for_each          = var.nlb_backend_cidrs
  type              = "ingress"
  ip_protocol       = "tcp"
  nic_type          = "intranet"
  policy            = "accept"
  port_range        = "30909/30909"
  priority          = 1
  security_group_id = var.worker_security_group_id
  cidr_ip           = each.value
}

resource "alicloud_nlb_server_group_server_attachment" "worker" {
  server_group_id = alicloud_nlb_server_group.receiver.id
  server_type     = "Ecs"
  server_id       = var.worker_instance_id
  port            = 30909
  weight          = 100
  depends_on      = [alicloud_security_group_rule.worker_nodeport]
}

resource "alicloud_nlb_listener" "receiver" {
  load_balancer_id  = alicloud_nlb_load_balancer.receiver.id
  listener_protocol = "TCP"
  listener_port     = 9091
  server_group_id   = alicloud_nlb_server_group.receiver.id
  depends_on        = [alicloud_nlb_server_group_server_attachment.worker]
}

# Post-2024 NLB DNS names require a customer CNAME. Keep the alias private to
# this VPC so Agents can use a stable internal hostname.
resource "alicloud_pvtz_zone" "receiver" {
  zone_name  = "pgcat-metrics.rdev.internal"
  tags       = local.tags
  depends_on = [terraform_data.account_guard]
}

resource "alicloud_pvtz_zone_record" "receiver" {
  zone_id = alicloud_pvtz_zone.receiver.id
  rr      = "write"
  type    = "CNAME"
  value   = alicloud_nlb_load_balancer.receiver.dns_name
  ttl     = 60
}

resource "alicloud_pvtz_zone_attachment" "receiver" {
  zone_id = alicloud_pvtz_zone.receiver.id
  vpcs {
    vpc_id    = var.vpc_id
    region_id = "ap-southeast-1"
  }
  depends_on = [alicloud_pvtz_zone_record.receiver]
}
