variable "account_id" {
  type = string
  validation {
    condition     = can(regex("^[0-9]{16}$", var.account_id))
    error_message = "Exact Alibaba account required."
  }
}
variable "vpc_id" {
  type = string
  validation {
    condition     = can(regex("^vpc-[A-Za-z0-9-]+$", var.vpc_id))
    error_message = "Exact VPC required."
  }
}
variable "nlb_zones" {
  type = list(object({ zone_id = string, vswitch_id = string }))
  validation {
    condition     = length(var.nlb_zones) >= 2 && length(distinct([for z in var.nlb_zones : z.zone_id])) == length(var.nlb_zones) && alltrue([for z in var.nlb_zones : startswith(z.zone_id, "ap-southeast-1") && can(regex("^vsw-[A-Za-z0-9-]+$", z.vswitch_id))])
    error_message = "Two distinct reviewed Singapore NLB zones required."
  }
}
variable "worker_instance_id" {
  type = string
  validation {
    condition     = can(regex("^i-[A-Za-z0-9]+$", var.worker_instance_id))
    error_message = "Exact ACK worker ECS ID required."
  }
}
variable "endpoint_zone_id" {
  type        = string
  description = "Zone whose private NLB address the local PgCat agents use."
  validation {
    condition     = contains([for z in var.nlb_zones : z.zone_id], var.endpoint_zone_id)
    error_message = "The receiver endpoint zone must be configured on the NLB."
  }
}
variable "worker_security_group_id" {
  type = string
  validation {
    condition     = can(regex("^sg-[A-Za-z0-9]+$", var.worker_security_group_id))
    error_message = "Exact ACK worker security group required."
  }
}
variable "proxy_client_cidrs" {
  type = set(string)
  validation {
    condition     = length(var.proxy_client_cidrs) > 0 && alltrue([for c in var.proxy_client_cidrs : can(cidrhost(c, 0)) && startswith(c, "10.70.") && tonumber(split("/", c)[1]) >= 24])
    error_message = "Explicit rdev PgCat source subnets required."
  }
}
variable "nlb_backend_cidrs" {
  type = set(string)
  validation {
    condition     = length(var.nlb_backend_cidrs) > 0 && alltrue([for c in var.nlb_backend_cidrs : can(cidrhost(c, 0)) && startswith(c, "10.70.") && tonumber(split("/", c)[1]) >= 24])
    error_message = "Explicit private NLB backend source subnets required."
  }
}
