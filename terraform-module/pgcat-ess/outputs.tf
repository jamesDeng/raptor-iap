output "group_id" { value = alicloud_ess_scaling_group.proxy.id }
output "server_group_id" { value = alicloud_nlb_server_group.proxy.id }
output "private_endpoint" { value = alicloud_nlb_load_balancer.proxy.dns_name }
output "association" { value = local.tags }
