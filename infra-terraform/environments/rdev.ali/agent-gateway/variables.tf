variable "rrsa_enabled" {
  type    = bool
  default = false
}
variable "oidc_provider_arn" {
  type    = string
  default = ""
}
variable "oidc_issuer" {
  type    = string
  default = ""
}

# Only public resource references belong here. The SQL password never enters
# this root or Terraform state; Gateway reads the encrypted OSS object privately.
variable "observer" {
  type = object({
    group_id         = string
    backend_group_id = string
    load_balancer_id = string
    sql_bucket       = string
    sql_key          = string
  })
  default = {
    group_id         = "asg-t4necb4dfmu6o3z2rn0g"
    backend_group_id = "sgp-xs5s42tihtl6ja2a67"
    load_balancer_id = "nlb-jqbxi42nurq0qkb7e9"
    sql_bucket       = "raptor-iap-rdev-pgcat-config-1360282071200743"
    sql_key          = "rdev.ali/pgcat-test/observer-sql.json"
  }
  description = "Verified dedicated rdev fleet references; explicit null disables initial observer provisioning. Runtime replacement remains separately disabled."
}
