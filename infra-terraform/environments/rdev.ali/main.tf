provider "alicloud" {
  region = "ap-southeast-1"
}
module "foundation" {
  source             = "../../../terraform-module/rdev-foundation"
  account_id         = var.account_id
  kubernetes_version = var.kubernetes_version
}
