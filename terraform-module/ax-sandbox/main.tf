data "alicloud_account" "current" {}
data "alicloud_vpcs" "platform" { ids = [var.vpc_id] }
locals {
  tags = { Project = "raptor-iap", Environment = "rdev.ali", Owner = "ax-sandbox" }
}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Authenticated account mismatch."
    }
    precondition {
      condition     = length(data.alicloud_vpcs.platform.vpcs) == 1 && try(data.alicloud_vpcs.platform.vpcs[0].cidr_block == "10.70.0.0/16" && data.alicloud_vpcs.platform.vpcs[0].tags["Project"] == "raptor-iap" && data.alicloud_vpcs.platform.vpcs[0].tags["Environment"] == "rdev.ali" && data.alicloud_vpcs.platform.vpcs[0].tags["Owner"] == "rdev-foundation", false)
      error_message = "Shared VPC ownership or CIDR mismatch."
    }
  }
}
resource "alicloud_vswitch" "workers" {
  vpc_id       = var.vpc_id
  zone_id      = "ap-southeast-1a"
  vswitch_name = "raptor-ax-workers"
  cidr_block   = "10.70.3.0/24"
  tags         = local.tags
  depends_on   = [terraform_data.account_guard]
}
resource "alicloud_cs_managed_kubernetes" "cluster" {
  name                           = "raptor-ax-sandbox"
  version                        = var.kubernetes_version
  cluster_spec                   = "ack.standard"
  profile                        = "Default"
  vswitch_ids                    = [alicloud_vswitch.workers.id]
  new_nat_gateway                = false
  slb_internet_enabled           = false
  pod_cidr                       = "10.74.0.0/16"
  service_cidr                   = "10.75.0.0/16"
  deletion_protection            = true
  skip_set_certificate_authority = true
  tags                           = local.tags
  addons { name = "flannel" }
}

# The pinned provider only applies slb_internet_enabled during cluster creation.
# Manage the public endpoint explicitly for an existing CLB-backed cluster.
resource "alicloud_eip_address" "api" {
  count                = var.public_api_enabled ? 1 : 0
  address_name         = "raptor-ax-api"
  bandwidth            = "5"
  internet_charge_type = "PayByTraffic"
  payment_type         = "PayAsYouGo"
  tags                 = local.tags
}
resource "alicloud_eip_association" "api" {
  count         = var.public_api_enabled ? 1 : 0
  allocation_id = alicloud_eip_address.api[0].id
  instance_id   = var.api_load_balancer_id
  instance_type = "SlbInstance"
}
