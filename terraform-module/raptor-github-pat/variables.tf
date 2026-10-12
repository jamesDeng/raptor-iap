variable "account_id" {
  type    = string
  default = "1360282071200743"
  validation {
    condition     = var.account_id == "1360282071200743"
    error_message = "Only the authorized rdev account is supported."
  }
}
variable "bucket" { type = string }
variable "oidc_provider_arn" { type = string }
variable "oidc_issuer" { type = string }
