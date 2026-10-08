variable "account_id" { type = string }
variable "vpc_id" { type = string }
variable "kubernetes_version" {
  type = string
  validation {
    condition     = var.kubernetes_version == "1.36.2-aliyun.1"
    error_message = "Only the live-verified compatibility candidate is allowed."
  }
}
