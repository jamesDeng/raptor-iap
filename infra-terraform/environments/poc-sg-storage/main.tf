provider "alicloud" { region = var.region }
module "storage_network" {
  source          = "../../../terraform-module/sandbox-storage-network"
  account_id      = var.account_id
  region          = var.region
  zone            = var.zone
  name_prefix     = var.name_prefix
  vpc_cidr        = var.vpc_cidr
  vswitch_cidr    = var.vswitch_cidr
  filesystem_id   = var.filesystem_id
  access_point_id = var.access_point_id
}
