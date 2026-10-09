# Component-owned access rules; omit only when caller supplies an existing group.
resource "alicloud_security_group" "proxy" {
  count               = var.security_group_id == null ? 1 : 0
  security_group_name = "raptor-test-${var.proxy_code}"
  vpc_id              = var.vpc_id
  tags                = local.tags
  depends_on          = [terraform_data.account_guard]
  lifecycle {
    precondition {
      condition     = length(var.sql_client_cidrs) > 0
      error_message = "A component-owned group needs explicit SQL clients."
    }
  }
}
resource "alicloud_security_group_rule" "sql" {
  for_each          = var.security_group_id == null ? var.sql_client_cidrs : toset([])
  type              = "ingress"
  ip_protocol       = "tcp"
  nic_type          = "intranet"
  policy            = "accept"
  port_range        = "6432/6432"
  priority          = 1
  security_group_id = alicloud_security_group.proxy[0].id
  cidr_ip           = each.value
}
resource "alicloud_security_group_rule" "metrics" {
  for_each          = var.security_group_id == null ? var.metrics_client_cidrs : toset([])
  type              = "ingress"
  ip_protocol       = "tcp"
  nic_type          = "intranet"
  policy            = "accept"
  port_range        = "9930/9930"
  priority          = 1
  security_group_id = alicloud_security_group.proxy[0].id
  cidr_ip           = each.value
}
