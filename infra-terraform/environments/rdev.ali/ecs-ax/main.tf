provider "alicloud" { region = "ap-southeast-1" }
module "node" {
  source              = "../../../../terraform-module/ecs-ax"
  ssh_public_key      = var.ssh_public_key
  admin_cidr          = var.admin_cidr
  gitops_state_bucket = "raptor-iap-tfstate-sg-200743"
  gitops_plan_role    = "raptor-iap-rdev-plan"
  gitops_apply_role   = "raptor-iap-rdev-apply"
}
