variable "region" {
  type    = string
  default = "ap-southeast-1"
  validation {
    condition     = var.region == "ap-southeast-1"
    error_message = "Only Singapore is allowed."
  }
}
variable "account_id" { type = string }
variable "name" { type = string }
variable "function_name" { type = string }
variable "cluster_id" {
  type    = string
  default = ""
}
variable "trigger_url" {
  type = string
  validation {
    condition     = startswith(var.trigger_url, "https://")
    error_message = "Protected FC3 HTTPS trigger URL required."
  }
}
variable "gateway_instance_id" {
  type        = string
  description = "Existing Singapore VPC_SHARED instance; never purchase an instance in this slice."
}

variable "ack_only" {
  type        = bool
  default     = false
  description = "Restrict application discovery to STS identity and the explicit ACK cluster."
  validation {
    condition     = !var.ack_only || var.cluster_id != ""
    error_message = "ACK-only mode requires a cluster ID."
  }
}
