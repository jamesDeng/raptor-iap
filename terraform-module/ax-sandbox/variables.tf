variable "account_id" { type = string }
variable "vpc_id" { type = string }
variable "kubernetes_version" {
  type = string
  validation {
    condition     = var.kubernetes_version == "1.36.2-aliyun.1"
    error_message = "Only the live-verified compatibility candidate is allowed."
  }
}

variable "public_api_enabled" {
  type        = bool
  default     = false
  description = "Explicitly enable the ACK public API endpoint."
}

variable "api_load_balancer_id" {
  type        = string
  default     = null
  description = "Verified ACK-owned private CLB to associate with the public API EIP."
  validation {
    condition     = !var.public_api_enabled || can(regex("^lb-", var.api_load_balancer_id))
    error_message = "Public API requires the verified ACK private CLB ID."
  }
}
