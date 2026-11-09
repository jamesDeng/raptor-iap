output "private_endpoint" {
  value = one([
    for mapping in alicloud_nlb_load_balancer.receiver.zone_mappings :
    mapping.private_ipv4_address if mapping.zone_id == var.endpoint_zone_id
  ])
  description = "Private IPv4 address of the receiver NLB in the selected client zone."
}
