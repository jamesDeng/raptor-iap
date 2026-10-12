provider "alicloud" { region = "ap-southeast-1" }
module "gateway" {
  source                    = "../../../../terraform-module/agent-gateway"
  team_id                   = "db28b89e-6cbd-53f8-a4a3-c33600d52aa7"
  volume_id                 = "34381ac2-c693-45d9-9a24-9ddbf7b91da2"
  bucket                    = "raptor-pi-auth-1360282071200743-20261004"
  gitops_state_bucket       = "raptor-iap-tfstate-sg-200743"
  gitops_plan_role          = "raptor-iap-rdev-plan"
  gitops_apply_role         = "raptor-iap-rdev-apply"
  rrsa_enabled              = var.rrsa_enabled
  oidc_provider_arn         = var.oidc_provider_arn
  oidc_issuer               = var.oidc_issuer
  observer_enabled          = var.observer != null
  observer_group_id         = var.observer != null ? var.observer.group_id : ""
  observer_backend_group_id = var.observer != null ? var.observer.backend_group_id : ""
  observer_load_balancer_id = var.observer != null ? var.observer.load_balancer_id : ""
  observer_sql_bucket       = var.observer != null ? var.observer.sql_bucket : ""
  observer_sql_key          = var.observer != null ? var.observer.sql_key : ""
}
