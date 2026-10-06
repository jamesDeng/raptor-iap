module "infra_api_read" {
  source              = "../../../terraform-module/infra-api-read"
  account_id          = "1360282071200743"
  name                = "raptor-rdev-infra"
  function_name       = "raptor-rdev-infra-read"
  gateway_instance_id = var.gateway_instance_id
  cluster_id          = "c92787e953503492ea141a744c81498f1"
  ack_only            = true
  trigger_url         = var.trigger_url
}
