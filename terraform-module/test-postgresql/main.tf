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
resource "alicloud_db_instance" "test" {
  engine                   = "PostgreSQL"
  engine_version           = var.engine_version
  category                 = "Basic"
  instance_type            = var.instance_class
  instance_storage         = var.storage_gib
  db_instance_storage_type = "general_essd"
  instance_charge_type     = "Postpaid"
  instance_name            = "raptor-test-${var.db_code}"
  zone_id                  = var.zone
  vpc_id                   = var.vpc_id
  vswitch_id               = var.vswitch_id
  ssl_action               = "Open"
  security_ips             = var.client_cidrs
  storage_auto_scale       = "Disable"
  deletion_protection      = true
  tags                     = { Project = "raptor-iap", Owner = "test-env-skills", env = var.env_code, db-code = var.db_code }
  depends_on               = [terraform_data.account_guard]
}
