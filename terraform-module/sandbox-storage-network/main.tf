data "alicloud_account" "current" {}
# timestamp() remains unknown while planning, ensuring a fresh identity read
# when a saved plan executes with its current provider credentials.
resource "terraform_data" "identity_refresh" {
  input = timestamp()
}
data "alicloud_account" "at_apply" {
  depends_on = [terraform_data.identity_refresh]
}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Authenticated account does not match the requested account."
    }
    precondition {
      condition     = data.alicloud_account.at_apply.id == var.account_id
      error_message = "Apply-time authenticated account does not match the requested account."
    }
  }
}
locals {
  tags = { Project = "raptor-iap", Environment = "poc-sg-storage" }
  rules = {
    deny_ingress = { direction = "ingress", protocol = "all", ports = "-1/-1", cidr = "0.0.0.0/0", policy = "drop", priority = 100 }
    deny_egress  = { direction = "egress", protocol = "all", ports = "-1/-1", cidr = "0.0.0.0/0", policy = "drop", priority = 100 }
    nfs          = { direction = "egress", protocol = "tcp", ports = "2049/2049", cidr = var.vpc_cidr, policy = "accept", priority = 1 }
    https        = { direction = "egress", protocol = "tcp", ports = "443/443", cidr = "0.0.0.0/0", policy = "accept", priority = 1 }
    dns_udp      = { direction = "egress", protocol = "udp", ports = "53/53", cidr = "0.0.0.0/0", policy = "accept", priority = 1 }
    dns_tcp      = { direction = "egress", protocol = "tcp", ports = "53/53", cidr = "0.0.0.0/0", policy = "accept", priority = 1 }
  }
}
resource "alicloud_vpc" "network" {
  vpc_name   = "${var.name_prefix}-vpc"
  cidr_block = var.vpc_cidr
  tags       = local.tags
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_vswitch" "sandbox" {
  vswitch_name = "${var.name_prefix}-zone-a"
  vpc_id       = alicloud_vpc.network.id
  zone_id      = var.zone
  cidr_block   = var.vswitch_cidr
}
resource "alicloud_security_group" "sandbox" {
  security_group_name = "${var.name_prefix}-sandbox"
  vpc_id              = alicloud_vpc.network.id
  inner_access_policy = "Drop"
  tags                = local.tags
}
resource "alicloud_security_group_rule" "rules" {
  for_each          = local.rules
  security_group_id = alicloud_security_group.sandbox.id
  type              = each.value.direction
  ip_protocol       = each.value.protocol
  port_range        = each.value.ports
  cidr_ip           = each.value.cidr
  policy            = each.value.policy
  priority          = each.value.priority
  nic_type          = "intranet"
}
resource "alicloud_ram_role" "execution" {
  role_name = var.name_prefix
  force     = false
  assume_role_policy_document = jsonencode({
    Version   = "1"
    Statement = [{ Effect = "Allow", Action = ["sts:AssumeRole"], Principal = { Service = ["fc.aliyuncs.com"] } }]
  })
  depends_on = [terraform_data.account_guard]
}

resource "alicloud_ram_policy" "storage" {
  count       = var.filesystem_id != null && var.access_point_id != null ? 1 : 0
  policy_name = "${var.name_prefix}-nas-data"
  force       = false
  policy_document = jsonencode({
    Version = "1"
    Statement = [{
      Effect    = "Allow"
      Action    = ["nas:ClientMount", "nas:ClientWrite", "nas:ClientRootAccess"]
      Resource  = ["acs:nas:${var.region}:${var.account_id}:filesystem/${var.filesystem_id}"]
      Condition = { StringEquals = { "nas:AccessPointArn" = "acs:nas:${var.region}:${var.account_id}:accesspoint/${var.access_point_id}" } }
    }]
  })
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_ram_role_policy_attachment" "storage" {
  count       = var.filesystem_id != null && var.access_point_id != null ? 1 : 0
  role_name   = alicloud_ram_role.execution.role_name
  policy_name = alicloud_ram_policy.storage[0].policy_name
  policy_type = "Custom"
}
