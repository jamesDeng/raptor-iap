module "infra_api_read" {
  source        = "../../../terraform-module/infra-api-read"
  account_id    = var.account_id
  name          = var.name
  function_name = var.function_name
  trigger_url   = var.trigger_url
  cluster_id    = var.cluster_id
}
