resource "alicloud_slb_load_balancer" "proxy" {
  load_balancer_name = "raptor-rdev-kong"
  address_type       = "internet"
  # Internet CLB is not subnet-bound; ACK CCM registers the owned VPC worker.
  master_zone_id       = "ap-southeast-1a"
  slave_zone_id        = "ap-southeast-1b"
  instance_charge_type = "PayByCLCU"
  internet_charge_type = "PayByTraffic"
  payment_type         = "PayAsYouGo"
  delete_protection    = "on"
  tags = {
    Project     = "raptor-iap"
    Environment = "rdev.ali"
    Owner       = "kong-ingress"
  }
  lifecycle {
    prevent_destroy = true
    # PayByCLCU reports its bandwidth ceiling; CCM adds the reuse marker.
    ignore_changes = [bandwidth, tags["kubernetes.reused.by.user"]]
  }
}
# ACK CCM owns the listener and backend groups. Do not declare them here.
resource "alicloud_alidns_record" "public" {
  for_each    = var.publish_dns ? toset(["raptor.rdev", "api.rdev"]) : toset([])
  domain_name = "raptor-iap.top"
  rr          = each.key
  type        = "A"
  value       = alicloud_slb_load_balancer.proxy.address
  ttl         = 600
}
