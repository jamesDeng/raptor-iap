# Intentionally no backend binding. Central owner supplies a dedicated state key
# and reviewed GitHub Actions execution; do not attach foundation state.
provider "alicloud" { region = "ap-southeast-1" }
module "database" {
  count          = var.enabled ? 1 : 0
  source         = "../../../terraform-module/test-postgresql"
  account_id     = try(var.stack.account_id, null)
  env_code       = try(var.stack.env_code, null)
  db_code        = try(var.stack.db_code, null)
  zone           = try(var.stack.db_zone, null)
  vpc_id         = try(var.stack.vpc_id, null)
  vswitch_id     = try(var.stack.db_vswitch_id, null)
  instance_class = try(var.stack.db_class, null)
  engine_version = try(var.stack.db_engine_version, null)
  storage_gib    = try(var.stack.db_storage_gib, null)
  client_cidrs   = try(var.stack.db_client_cidrs, null)
}
module "proxy" {
  count               = var.enabled ? 1 : 0
  source              = "../../../terraform-module/pgcat-ess"
  account_id          = try(var.stack.account_id, null)
  env_code            = try(var.stack.env_code, null)
  proxy_code          = try(var.stack.proxy_code, null)
  target_db_code      = try(var.stack.db_code, null)
  target_db_env       = try(var.stack.env_code, null)
  target_db_host      = var.enabled ? module.database[0].private_endpoint : null
  target_database     = try(var.stack.target_database, null)
  vpc_id              = try(var.stack.vpc_id, null)
  vswitch_ids         = try(var.stack.proxy_vswitch_ids, null)
  nlb_zones           = try(var.stack.nlb_zones, null)
  security_group_id   = try(var.stack.proxy_security_group_id, null)
  image_id            = try(var.stack.proxy_image_id, null)
  instance_class      = try(var.stack.proxy_instance_class, null)
  secret_reference    = try(var.stack.secret_reference, null)
  execution_role_name = try(var.stack.execution_role_name, null)
  bootstrap_revision  = try(var.stack.bootstrap_revision, null)
  bootstrap_reviewed  = try(var.stack.bootstrap_reviewed, false)
}
