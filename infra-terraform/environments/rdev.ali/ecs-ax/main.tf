provider "alicloud" { region = "ap-southeast-1" }
module "node" {
  source         = "../../../../terraform-module/ecs-ax"
  ssh_public_key = var.ssh_public_key
  admin_cidr     = var.admin_cidr
}
