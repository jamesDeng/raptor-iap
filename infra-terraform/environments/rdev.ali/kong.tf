# Remains disabled until a complete price receipt and exact action plan pass.
variable "enable_kong_ingress" {
  type    = bool
  default = false
}
variable "publish_kong_dns" {
  type    = bool
  default = false
}
module "kong_ingress" {
  count       = var.enable_kong_ingress ? 1 : 0
  source      = "../../../terraform-module/kong-ingress"
  vswitch_id  = "vsw-t4nop9qf6v46gw2sa8l7d"
  publish_dns = var.publish_kong_dns
}
output "kong_ingress" {
  value = var.enable_kong_ingress ? {
    load_balancer_id = module.kong_ingress[0].load_balancer_id
    public_address   = module.kong_ingress[0].public_address
  } : null
}
