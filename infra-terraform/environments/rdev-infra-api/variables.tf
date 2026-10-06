variable "trigger_url" {
  type = string
  validation {
    condition     = can(regex("^https://[a-zA-Z0-9.-]+\\.ap-southeast-1\\.fcapp\\.run/?$", var.trigger_url))
    error_message = "Verified Singapore FC3 trigger URL required."
  }
}

variable "gateway_instance_id" {
  type        = string
  description = "Privately supplied, freshly verified existing Singapore shared gateway ID."
  validation {
    condition     = can(regex("^api-shared-vpc-[a-z0-9-]+$", var.gateway_instance_id))
    error_message = "An existing shared VPC gateway ID is required; no purchased gateway is created."
  }
}
