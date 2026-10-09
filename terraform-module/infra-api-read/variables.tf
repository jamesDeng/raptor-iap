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

variable "enable_rds_discovery" {
  type        = bool
  default     = false
  description = "Add RDS instance and tag reads to the otherwise ACK-only execution role."
}

variable "enable_proxy_commands" {
  type    = bool
  default = false
  validation {
    condition     = !var.enable_proxy_commands || (length(var.proxy_targets) > 0 && var.cluster_id != "")
    error_message = "Proxy runtime requires explicit targets and the private ACK cluster."
  }
}
variable "proxy_targets" {
  type    = map(object({ group_id = string, server_group_id = string, load_balancer_id = string }))
  default = {}
  validation {
    condition     = alltrue([for p in values(var.proxy_targets) : can(regex("^asg-[a-zA-Z0-9-]+$", p.group_id)) && can(regex("^sgp-[a-zA-Z0-9-]+$", p.server_group_id)) && can(regex("^nlb-[a-zA-Z0-9-]+$", p.load_balancer_id))])
    error_message = "Only explicit ESS/server-group/load-balancer IDs are allowed."
  }
}
variable "gitops_plan_role" {
  type    = string
  default = ""
  validation {
    condition     = (var.gitops_plan_role == "" && var.gitops_apply_role == "") || (var.gitops_plan_role != "" && var.gitops_apply_role != "" && var.gitops_state_bucket != "" && var.gitops_plan_role != var.gitops_apply_role)
    error_message = "GitOps requires separate existing roles and a state bucket."
  }
}
variable "gitops_apply_role" {
  type    = string
  default = ""
  validation {
    condition     = var.gitops_apply_role == "" || can(regex("^[a-zA-Z0-9-]+$", var.gitops_apply_role))
    error_message = "GitOps apply requires the planning role."
  }
}
variable "gitops_state_bucket" {
  type    = string
  default = ""
  validation {
    condition     = var.gitops_state_bucket == "" || can(regex("^[a-z0-9-]{3,63}$", var.gitops_state_bucket))
    error_message = "Valid existing state bucket required."
  }
}
