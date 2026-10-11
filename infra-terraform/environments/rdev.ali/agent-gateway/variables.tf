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
  default     = null
  description = "Opt-in dedicated fleet observation; null preserves question-only deployment."
}
