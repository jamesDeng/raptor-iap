provider "alicloud" {
  region = "ap-southeast-1"
}
module "foundation" {
  source                 = "../../../terraform-module/rdev-foundation"
  enable_rrsa            = var.enable_rrsa
  account_id             = var.account_id
  kubernetes_version     = var.kubernetes_version
  platform_database_code = "D00001"
}
