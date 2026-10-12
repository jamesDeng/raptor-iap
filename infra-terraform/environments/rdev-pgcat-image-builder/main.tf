provider "alicloud" { region = "ap-southeast-1" }
module "builder" {
  source     = "../../../terraform-module/pgcat-ess/modules/image-builder"
  account_id = "1360282071200743"
  vpc_id     = "vpc-t4n4fi6r1a7bi6n93ftq3"
  vswitch_id = "vsw-t4nop9qf6v46gw2sa8l7d"
}
output "instance_id" { value = module.builder.instance_id }
