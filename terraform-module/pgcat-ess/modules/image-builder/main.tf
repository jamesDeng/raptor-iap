data "alicloud_account" "current" {}
resource "alicloud_security_group" "builder" {
  security_group_name = "raptor-rdev-pgcat-image-builder"
  description         = "Temporary private PgCat image builder; no ingress rules"
  vpc_id              = var.vpc_id
  tags                = local.tags
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Wrong Alibaba Cloud account."
    }
  }
}
locals {
  tags = { Project = "raptor-iap", env = "rdev.ali", Purpose = "pgcat-image-builder" }
}
resource "alicloud_instance" "builder" {
  instance_name              = "raptor-rdev-pgcat-image-builder"
  instance_charge_type       = "PostPaid"
  instance_type              = "ecs.e-c1m2.large"
  image_id                   = "ubuntu_24_04_x64_20G_alibase_20260916.vhd"
  vswitch_id                 = var.vswitch_id
  security_groups            = [alicloud_security_group.builder.id]
  internet_max_bandwidth_out = 0
  system_disk_category       = "cloud_essd"
  system_disk_size           = 20
  system_disk_encrypted      = true
  tags                       = local.tags
}
