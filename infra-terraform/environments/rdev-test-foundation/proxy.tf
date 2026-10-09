# Default remains disabled; protected GitOps inputs bind the actual OSS version.
module "proxy" {
  count                = var.proxy_enabled ? 1 : 0
  source               = "../../../terraform-module/pgcat-ess"
  account_id           = var.account_id
  env_code             = "rdev.ali"
  proxy_code           = var.proxy_code
  target_db_code       = var.db_code
  target_db_env        = "rdev.ali"
  target_db_host       = module.database[0].private_endpoint
  target_database      = "infra_poc"
  vpc_id               = var.vpc_id
  vswitch_ids          = [var.proxy_worker_vswitch_id]
  nlb_zones            = [{ zone_id = "ap-southeast-1a", vswitch_id = var.proxy_worker_vswitch_id }, { zone_id = "ap-southeast-1b", vswitch_id = alicloud_vswitch.proxy_b.id }]
  sql_client_cidrs     = ["10.70.1.0/24", "10.70.4.0/24"]
  metrics_client_cidrs = ["10.70.1.0/24"]
  image_id             = "ubuntu_24_04_x64_20G_alibase_20260916.vhd"
  container_image      = "ghcr.io/jamesdeng/raptor-pgcat@sha256:cc29d36a4437eeda529966c4fceba7f330db152e7bc08e2932f17b944805f0c8"
  instance_class       = "ecs.e-c1m2.large"
  secret_reference     = "oss://${var.config_bucket}/rdev.ali/pgcat-test/config.json?versionId=${var.proxy_secret_version}"
  execution_role_name  = "raptor-iap-rdev-pgcat-config"
  bootstrap_revision   = "744951df8415b4f4cfd4b78a6d7bb6e994ed11c2"
  bootstrap_reviewed   = true # reviewed artifact/config binding, not a claim of live ECS boot
}
# Adopt existing IAM. Retire the temporary pgcat-iam state owner before APPLY.
import {
  for_each = var.proxy_enabled ? toset(["config"]) : toset([])
  to       = module.proxy[0].module.config_iam.alicloud_ram_role.config
  id       = "raptor-iap-rdev-pgcat-config"
}
import {
  for_each = var.proxy_enabled ? toset(["config"]) : toset([])
  to       = module.proxy[0].module.config_iam.alicloud_ram_policy.config
  id       = "raptor-iap-rdev-pgcat-config-read"
}
import {
  for_each = var.proxy_enabled ? toset(["config"]) : toset([])
  to       = module.proxy[0].module.config_iam.alicloud_ram_role_policy_attachment.config
  id       = "role:raptor-iap-rdev-pgcat-config-read:Custom:raptor-iap-rdev-pgcat-config"
}
output "proxy_endpoint" { value = var.proxy_enabled ? module.proxy[0].private_endpoint : null }
output "proxy_group_id" { value = var.proxy_enabled ? module.proxy[0].group_id : null }
