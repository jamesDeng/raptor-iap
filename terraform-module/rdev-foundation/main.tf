data "alicloud_account" "current" {}
locals {
  tags = { Project = "raptor-iap", Environment = "rdev.ali", Owner = "rdev-foundation" }
}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Authenticated account does not match."
    }
  }
}
resource "alicloud_vpc" "env" {
  vpc_name   = "raptor-rdev"
  cidr_block = "10.70.0.0/16"
  tags       = local.tags
  depends_on = [terraform_data.account_guard]
}
resource "alicloud_vswitch" "workers" {
  vpc_id       = alicloud_vpc.env.id
  zone_id      = var.zone
  vswitch_name = "raptor-rdev-workers"
  cidr_block   = "10.70.1.0/24"
}
resource "alicloud_vswitch" "database" {
  vpc_id       = alicloud_vpc.env.id
  zone_id      = var.zone
  vswitch_name = "raptor-rdev-database"
  cidr_block   = "10.70.2.0/24"
}
resource "alicloud_nat_gateway" "outbound" {
  vpc_id               = alicloud_vpc.env.id
  vswitch_id           = alicloud_vswitch.workers.id
  nat_gateway_name     = "raptor-rdev-outbound"
  nat_type             = "Enhanced"
  payment_type         = "PayAsYouGo"
  internet_charge_type = "PayByLcu"
  deletion_protection  = true
  tags                 = local.tags
}
resource "alicloud_eip_address" "outbound" {
  address_name         = "raptor-rdev-outbound"
  payment_type         = "PayAsYouGo"
  internet_charge_type = "PayByTraffic"
  bandwidth            = "5"
  tags                 = local.tags
}
resource "alicloud_eip_association" "outbound" {
  allocation_id = alicloud_eip_address.outbound.id
  instance_id   = alicloud_nat_gateway.outbound.id
  instance_type = "Nat"
}
resource "alicloud_snat_entry" "workers" {
  snat_table_id     = alicloud_nat_gateway.outbound.snat_table_ids
  source_vswitch_id = alicloud_vswitch.workers.id
  snat_ip           = alicloud_eip_address.outbound.ip_address
  depends_on        = [alicloud_eip_association.outbound]
}
resource "alicloud_cs_managed_kubernetes" "cluster" {
  name                 = "raptor-rdev"
  version              = var.kubernetes_version
  cluster_spec         = "ack.standard"
  profile              = "Default"
  vswitch_ids          = [alicloud_vswitch.workers.id]
  new_nat_gateway      = false
  slb_internet_enabled = false
  pod_cidr             = "10.72.0.0/16"
  service_cidr         = "10.73.0.0/16"
  deletion_protection  = true
  tags                 = local.tags
  addons {
    name = "flannel"
  }
  depends_on = [alicloud_snat_entry.workers]
}
resource "alicloud_cs_kubernetes_node_pool" "platform" {
  cluster_id                 = alicloud_cs_managed_kubernetes.cluster.id
  node_pool_name             = "platform"
  vswitch_ids                = [alicloud_vswitch.workers.id]
  instance_types             = ["ecs.e-c1m2.xlarge"]
  desired_size               = "1"
  instance_charge_type       = "PostPaid"
  system_disk_category       = "cloud_essd"
  system_disk_size           = 40
  internet_max_bandwidth_out = 0
  spot_strategy              = "NoSpot"
  tags                       = local.tags
}
resource "alicloud_db_instance" "platform" {
  engine                   = "PostgreSQL"
  engine_version           = "14.0"
  category                 = "Basic"
  instance_type            = "pg.n2e.1c.1m"
  instance_storage         = 10
  db_instance_storage_type = "general_essd"
  instance_charge_type     = "Postpaid"
  instance_name            = "raptor-rdev-platform"
  zone_id                  = var.zone
  vpc_id                   = alicloud_vpc.env.id
  vswitch_id               = alicloud_vswitch.database.id
  security_ips             = ["10.70.1.0/24", "10.72.0.0/16"]
  storage_auto_scale       = "Disable"
  deletion_protection      = true
  tags                     = local.tags
}
