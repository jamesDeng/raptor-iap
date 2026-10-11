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
  # Existing resources: no instance or deployment recreation is authorized.
  catalog_untag_targets = {
    scaling_group_id  = "asg-t4necb4dfmu6o3z2rn0g"
    security_group_id = "sg-t4ncnd03mvg78yw36g1j"
    load_balancer_id  = "nlb-jqbxi42nurq0qkb7e9"
    server_group_id   = "sgp-xs5s42tihtl6ja2a67"
    db_instance_id    = "pgm-t4nq66x7pr6s55w6"
  }
}
