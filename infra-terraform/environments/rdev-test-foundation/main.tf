terraform {
  required_version = "= 1.13.3"
  required_providers { alicloud = { source = "aliyun/alicloud", version = "= 1.293.0" } }
  backend "oss" {}
}
provider "alicloud" { region = "ap-southeast-1" }
data "alicloud_account" "current" {}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Wrong POC account."
    }
  }
}
locals { tags = { Project = "raptor-iap", env = "rdev.ali", Owner = "test-env" } }
resource "alicloud_vswitch" "proxy_b" {
  vpc_id       = var.vpc_id
  zone_id      = "ap-southeast-1b"
  cidr_block   = "10.70.4.0/24"
  vswitch_name = "raptor-rdev-pgcat-b"
  tags         = local.tags
  depends_on   = [terraform_data.account_guard]
}
resource "alicloud_oss_bucket" "config" {
  bucket        = var.config_bucket
  force_destroy = false
  server_side_encryption_rule { sse_algorithm = "AES256" }
  versioning { status = "Enabled" }
  tags = local.tags
  lifecycle { prevent_destroy = true }
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_oss_bucket_acl" "config" {
  bucket = alicloud_oss_bucket.config.bucket
  acl    = "private"
}
module "database" {
  count          = 1
  source         = "../../../terraform-module/test-postgresql"
  account_id     = var.account_id
  env_code       = "rdev.ali"
  db_code        = var.db_code
  zone           = "ap-southeast-1a"
  vpc_id         = var.vpc_id
  vswitch_id     = var.db_vswitch_id
  instance_class = "pg.n2e.1c.1m"
  engine_version = "14.0"
  storage_gib    = 10
  client_cidrs   = ["10.70.1.0/24", "10.70.4.0/24"]
}
output "database_endpoint" { value = module.database[0].private_endpoint }
output "database_id" { value = module.database[0].resource_id }
output "proxy_zone_b_vswitch" { value = alicloud_vswitch.proxy_b.id }
