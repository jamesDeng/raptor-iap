terraform {
  required_version = "= 1.13.3"
  required_providers { alicloud = { source = "aliyun/alicloud", version = "= 1.293.0" } }
  backend "oss" {}
}
provider "alicloud" { region = "ap-southeast-1" }
variable "account_id" { type = string }
variable "state_bucket" { type = string }
variable "config_bucket" { type = string }
module "access" {
  source          = "../../../terraform-module/test-gitops-access"
  account_id      = var.account_id
  state_bucket    = var.state_bucket
  config_bucket   = var.config_bucket
  plan_role_name  = "raptor-iap-rdev-plan"
  apply_role_name = "raptor-iap-rdev-apply"
}
