data "alicloud_account" "current" {}
resource "terraform_data" "identity_refresh" { input = timestamp() }
data "alicloud_account" "at_apply" { depends_on = [terraform_data.identity_refresh] }
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Plan-time account mismatch."
    }
    precondition {
      condition     = data.alicloud_account.at_apply.id == var.account_id
      error_message = "Apply-time account mismatch."
    }
  }
}
locals {
  tags      = { Project = "raptor-iap", Owner = "test-env-skills", env = var.env_code, db-proxy-code = var.proxy_code, target-db-code = var.target_db_code }
  bootstrap = jsonencode({ env = var.env_code, proxy_code = var.proxy_code, target_db_code = var.target_db_code, db_host = var.target_db_host, database = var.target_database, secret_reference = var.secret_reference, revision = var.bootstrap_revision })
}
# Shared Alibaba-managed NLB prerequisite, retained when this POC is removed.
resource "alicloud_resource_manager_service_linked_role" "nlb" {
  service_name = "nlb.aliyuncs.com"
  lifecycle { prevent_destroy = true }
}
resource "alicloud_nlb_load_balancer" "proxy" {
  load_balancer_name          = "raptor-test-${var.proxy_code}"
  address_type                = "Intranet"
  address_ip_version          = "Ipv4"
  load_balancer_type          = "Network"
  payment_type                = "PayAsYouGo"
  vpc_id                      = var.vpc_id
  deletion_protection_enabled = true
  tags                        = local.tags
  dynamic "zone_mappings" {
    for_each = var.nlb_zones
    content {
      zone_id    = zone_mappings.value.zone_id
      vswitch_id = zone_mappings.value.vswitch_id
    }
  }
  depends_on = [terraform_data.account_guard, alicloud_resource_manager_service_linked_role.nlb]
}
resource "alicloud_nlb_server_group" "proxy" {
  server_group_name          = "raptor-test-${var.proxy_code}"
  server_group_type          = "Instance"
  vpc_id                     = var.vpc_id
  protocol                   = "TCP"
  connection_drain_enabled   = false
  preserve_client_ip_enabled = false
  tags                       = local.tags
  health_check {
    health_check_enabled      = true
    health_check_type         = "TCP"
    health_check_connect_port = 6432
  }
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_nlb_listener" "proxy" {
  load_balancer_id  = alicloud_nlb_load_balancer.proxy.id
  listener_protocol = "TCP"
  listener_port     = 6432
  server_group_id   = alicloud_nlb_server_group.proxy.id
}
resource "alicloud_ess_scaling_group" "proxy" {
  scaling_group_name        = "raptor-test-${var.proxy_code}"
  min_size                  = 2
  max_size                  = 4
  desired_capacity          = 2
  vswitch_ids               = var.vswitch_ids
  tags                      = local.tags
  group_deletion_protection = true
  depends_on                = [terraform_data.account_guard]
  lifecycle { ignore_changes = [desired_capacity] }
}
resource "alicloud_ess_server_group_attachment" "proxy" {
  scaling_group_id = alicloud_ess_scaling_group.proxy.id
  server_group_id  = alicloud_nlb_server_group.proxy.id
  type             = "NLB"
  port             = 6432
  weight           = 100
  force_attach     = false
}
resource "alicloud_ess_scaling_configuration" "proxy" {
  scaling_group_id           = alicloud_ess_scaling_group.proxy.id
  scaling_configuration_name = "raptor-test-${var.proxy_code}"
  image_id                   = var.image_id
  instance_type              = var.instance_class
  security_group_id          = var.security_group_id != null ? var.security_group_id : alicloud_security_group.proxy[0].id
  role_name                  = module.config_iam.role_name
  internet_max_bandwidth_out = 0
  system_disk_category       = "cloud_essd"
  system_disk_size           = 20
  system_disk_encrypted      = true
  spot_strategy              = "NoSpot"
  active                     = true
  enable                     = true
  tags                       = local.tags
  user_data = base64encode(templatefile("${path.module}/startup.sh.tftpl", {
    metadata_b64    = base64encode(local.bootstrap)
    container_image = var.container_image
  }))
  depends_on = [alicloud_ess_server_group_attachment.proxy]
}

module "config_iam" {
  source      = "./modules/config-iam"
  account_id  = var.account_id
  role_name   = var.execution_role_name
  bucket_name = regex("^oss://([^/]+)/", var.secret_reference)[0]
  object_key  = regex("^oss://[^/]+/([^?]+)\\?versionId=", var.secret_reference)[0]
}
