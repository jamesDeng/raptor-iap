variable "account_id" {
  type = string
}
variable "platform_database_code" {
  type        = string
  description = "Existing Raptor catalog code for the platform PostgreSQL instance."
  default     = ""
}
variable "kubernetes_version" {
  type        = string
  description = "Version independently verified available in Singapore; no assumed default."
}
variable "zone" {
  type    = string
  default = "ap-southeast-1a"
  validation {
    condition     = startswith(var.zone, "ap-southeast-1")
    error_message = "Only Singapore zones are allowed."
  }
}

variable "enable_rrsa" {
  type    = bool
  default = false
}
