provider "alicloud" { region = "ap-southeast-1" }
module "state" {
  source             = "../../../terraform-module/rdev-state"
  account_id         = var.account_id
  bucket_name        = var.bucket_name
  lock_instance_name = var.lock_instance_name
}
