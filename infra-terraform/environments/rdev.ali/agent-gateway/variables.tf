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
