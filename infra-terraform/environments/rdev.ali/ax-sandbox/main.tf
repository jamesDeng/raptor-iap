provider "alicloud" { region = "ap-southeast-1" }
module "sandbox" {
  source             = "../../../../terraform-module/ax-sandbox"
  account_id         = var.account_id
  vpc_id             = var.vpc_id
  kubernetes_version = "1.36.2-aliyun.1"
}
