# Terraform owns the private NLB, its precise worker backend and both security
# boundaries. The chart owns only the in-cluster write-only NodePort service.
module "metrics_receiver" {
  count                    = var.metrics_receiver_enabled ? 1 : 0
  source                   = "../../../terraform-module/infra-test-metrics-receiver"
  account_id               = var.account_id
  vpc_id                   = var.vpc_id
  nlb_zones                = [{ zone_id = "ap-southeast-1a", vswitch_id = var.proxy_worker_vswitch_id }, { zone_id = "ap-southeast-1b", vswitch_id = alicloud_vswitch.proxy_b.id }]
  endpoint_zone_id         = "ap-southeast-1a"
  worker_instance_id       = var.receiver_worker_instance_id
  worker_security_group_id = var.receiver_worker_security_group_id
  proxy_client_cidrs       = ["10.70.1.0/24"]
  nlb_backend_cidrs        = ["10.70.1.0/24", "10.70.4.0/24"]
}

output "metrics_receiver_endpoint" {
  value = var.metrics_receiver_enabled ? module.metrics_receiver[0].private_endpoint : null
}
