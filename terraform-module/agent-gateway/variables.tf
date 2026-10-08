variable "account_id" {
  type    = string
  default = "1360282071200743"
  validation {
    condition     = var.account_id == "1360282071200743"
    error_message = "Only the authorized rdev account is supported."
  }
}
variable "team_id" { type = string }
variable "volume_id" { type = string }
variable "bucket" { type = string }
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
