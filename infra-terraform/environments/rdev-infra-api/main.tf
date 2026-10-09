module "infra_api_read" {
  source                = "../../../terraform-module/infra-api-read"
  account_id            = "1360282071200743"
  name                  = "raptor-rdev-infra"
  function_name         = "raptor-rdev-infra-read"
  gateway_instance_id   = var.gateway_instance_id
  cluster_id            = "c92787e953503492ea141a744c81498f1"
  ack_only              = true
  enable_rds_discovery  = true
  trigger_url           = var.trigger_url
  gitops_plan_role      = "raptor-iap-rdev-plan"
  gitops_apply_role     = "raptor-iap-rdev-apply"
  gitops_state_bucket   = "raptor-iap-tfstate-sg-200743"
  deployment_user       = "raptor-rdev-infra-deploy"
  enable_proxy_commands = true
  proxy_targets = {
    "b5267fa4-33b9-408c-a799-0ed56501061f" = {
      group_id         = "asg-t4necb4dfmu6o3z2rn0g"
      server_group_id  = "sgp-xs5s42tihtl6ja2a67"
      load_balancer_id = "nlb-jqbxi42nurq0qkb7e9"
    }
  }
}
