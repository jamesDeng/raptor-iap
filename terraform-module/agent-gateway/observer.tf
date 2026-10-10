# Observer credentials are short-lived STS sessions assumed by the controller.
# The sandbox never receives the controller identity or provisioning permissions.
variable "observer_enabled" {
  type    = bool
  default = false
}
variable "observer_group_id" {
  type    = string
  default = ""
  validation {
    condition     = var.observer_group_id == "" || can(regex("^asg-[a-zA-Z0-9]+$", var.observer_group_id))
    error_message = "A concrete ESS scaling group ID is required."
  }
}
variable "observer_backend_group_id" {
  type    = string
  default = ""
  validation {
    condition     = var.observer_backend_group_id == "" || can(regex("^sgp-[a-zA-Z0-9]+$", var.observer_backend_group_id))
    error_message = "A concrete NLB backend group ID is required."
  }
}
variable "observer_load_balancer_id" {
  type    = string
  default = ""
  validation {
    condition     = var.observer_load_balancer_id == "" || can(regex("^nlb-[a-zA-Z0-9]+$", var.observer_load_balancer_id))
    error_message = "A concrete NLB ID is required."
  }
}
variable "observer_sql_bucket" {
  type    = string
  default = ""
  validation {
    condition     = var.observer_sql_bucket == "" || can(regex("^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$", var.observer_sql_bucket))
    error_message = "A concrete private OSS bucket is required."
  }
}
variable "observer_sql_key" {
  type    = string
  default = ""
  validation {
    condition     = var.observer_sql_key == "" || (can(regex("^[A-Za-z0-9][A-Za-z0-9_./-]{0,255}$", var.observer_sql_key)) && !strcontains(var.observer_sql_key, ".."))
    error_message = "A concrete OSS object key without traversal or wildcards is required."
  }
}
locals {
  observer_read_statements = concat([{ Effect = "Allow", Action = ["ess:DescribeScalingInstances", "ess:DescribeScalingGroups"], Resource = ["acs:ess:ap-southeast-1:${var.account_id}:scalinggroup/${var.observer_group_id}"] }], var.observer_backend_group_id != "" && var.observer_load_balancer_id != "" ? [
    { Effect = "Allow", Action = ["nlb:ListServerGroupServers"], Resource = ["acs:nlb:ap-southeast-1:${var.account_id}:serverGroup/${var.observer_backend_group_id}"] },
    { Effect = "Allow", Action = ["nlb:GetListenerAttribute", "nlb:GetListenerHealthStatus"], Resource = ["acs:nlb:ap-southeast-1:${var.account_id}:loadbalancer/${var.observer_load_balancer_id}"] }
  ] : [])
}
resource "terraform_data" "observer_guard" {
  count = var.observer_enabled ? 1 : 0
  lifecycle {
    precondition {
      condition     = var.observer_group_id != "" && ((var.observer_backend_group_id == "") == (var.observer_load_balancer_id == "")) && ((var.observer_sql_bucket == "") == (var.observer_sql_key == ""))
      error_message = "Observer enablement requires the verified dedicated fleet ID."
    }
  }
}
resource "alicloud_ram_role" "observer" {
  count                       = var.observer_enabled ? 1 : 0
  role_name                   = "raptor-rdev-agent-observer"
  description                 = "Request-scoped read-only PgCat fleet observation"
  max_session_duration        = 3600
  assume_role_policy_document = jsonencode({ Version = "1", Statement = [{ Effect = "Allow", Action = "sts:AssumeRole", Principal = { RAM = ["acs:ram::${var.account_id}:role/${local.role_name}"] } }] })
  depends_on                  = [terraform_data.guard, terraform_data.observer_guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_policy" "observer" {
  count           = var.observer_enabled ? 1 : 0
  policy_name     = "raptor-rdev-agent-observer"
  policy_document = jsonencode({ Version = "1", Statement = local.observer_read_statements })
  depends_on      = [terraform_data.guard, terraform_data.observer_guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_role_policy_attachment" "observer" {
  count       = var.observer_enabled ? 1 : 0
  role_name   = alicloud_ram_role.observer[0].role_name
  policy_name = alicloud_ram_policy.observer[0].policy_name
  policy_type = "Custom"
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_policy" "observer_assume" {
  count       = var.observer_enabled ? 1 : 0
  policy_name = "raptor-rdev-gateway-assume-observer"
  policy_document = jsonencode({ Version = "1", Statement = concat([{ Effect = "Allow", Action = ["sts:AssumeRole"], Resource = ["acs:ram::${var.account_id}:role/raptor-rdev-agent-observer"] }], var.observer_sql_bucket != "" && var.observer_sql_key != "" ? [
    { Effect = "Allow", Action = ["oss:GetBucketEncryption"], Resource = ["acs:oss:*:${var.account_id}:${var.observer_sql_bucket}"] },
    { Effect = "Allow", Action = ["oss:GetObject"], Resource = ["acs:oss:*:${var.account_id}:${var.observer_sql_bucket}/${var.observer_sql_key}"] }
  ] : []) })
  depends_on = [terraform_data.guard]
  lifecycle { prevent_destroy = true }
}
resource "alicloud_ram_role_policy_attachment" "observer_assume" {
  count       = var.observer_enabled ? 1 : 0
  role_name   = alicloud_ram_role.controller.role_name
  policy_name = alicloud_ram_policy.observer_assume[0].policy_name
  policy_type = "Custom"
  lifecycle { prevent_destroy = true }
}
output "observer_role_arn" {
  value = var.observer_enabled ? "acs:ram::${var.account_id}:role/raptor-rdev-agent-observer" : null
}
