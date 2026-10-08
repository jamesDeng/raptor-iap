data "alicloud_account" "current" {}
data "alicloud_vswitches" "target" { ids = ["vsw-t4nop9qf6v46gw2sa8l7d"] }
locals { tags = { Project = "raptor-iap", Environment = "rdev.ali", Owner = "ecs-ax" } }
resource "terraform_data" "guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == "1360282071200743" && length(data.alicloud_vswitches.target.vswitches) == 1 && try(data.alicloud_vswitches.target.vswitches[0].vpc_id == "vpc-t4n4fi6r1a7bi6n93ftq3" && data.alicloud_vswitches.target.vswitches[0].cidr_block == "10.70.1.0/24", false)
      error_message = "Authenticated account or existing rdev subnet mismatch."
    }
  }
}
resource "alicloud_security_group" "node" {
  security_group_name = "raptor-ecs-ax"
  vpc_id              = "vpc-t4n4fi6r1a7bi6n93ftq3"
  tags                = local.tags
  depends_on          = [terraform_data.guard]
}
resource "alicloud_security_group_rule" "ssh" {
  type              = "ingress"
  ip_protocol       = "tcp"
  nic_type          = "intranet"
  policy            = "accept"
  port_range        = "22/22"
  security_group_id = alicloud_security_group.node.id
  cidr_ip           = var.admin_cidr
}
resource "alicloud_security_group_rule" "private" {
  for_each          = toset(["6443/6443", "8443/8443", "8080/8080"])
  type              = "ingress"
  ip_protocol       = "tcp"
  nic_type          = "intranet"
  policy            = "accept"
  port_range        = each.key
  security_group_id = alicloud_security_group.node.id
  cidr_ip           = "10.70.0.0/16"
}
resource "alicloud_ecs_key_pair" "node" {
  depends_on    = [terraform_data.guard]
  key_pair_name = "raptor-ecs-ax"
  public_key    = var.ssh_public_key
  tags          = local.tags
}
resource "alicloud_instance" "node" {
  instance_name              = "raptor-ecs-ax"
  host_name                  = "raptor-ecs-ax"
  image_id                   = "ubuntu_24_04_x64_20G_alibase_20260916.vhd"
  instance_type              = "ecs.g7.xlarge"
  instance_charge_type       = "PostPaid"
  vswitch_id                 = "vsw-t4nop9qf6v46gw2sa8l7d"
  security_groups            = [alicloud_security_group.node.id]
  key_name                   = alicloud_ecs_key_pair.node.key_pair_name
  system_disk_category       = "cloud_essd"
  system_disk_size           = 120
  system_disk_encrypted      = true
  internet_charge_type       = "PayByTraffic"
  internet_max_bandwidth_out = 5
  deletion_protection        = true
  tags                       = local.tags
}
