variable "account_id" { type = string }
variable "vpc_id" { type = string }
variable "db_vswitch_id" { type = string }
variable "config_bucket" { type = string }
variable "db_code" { type = string }

variable "proxy_enabled" {
  type    = bool
  default = false
}

variable "proxy_code" {
  type    = string
  default = ""
}

variable "proxy_worker_vswitch_id" {
  type    = string
  default = ""
}

variable "proxy_secret_version" {
  type    = string
  default = ""
}
variable "metrics_receiver_enabled" {
  type    = bool
  default = false
  validation {
    condition     = !var.metrics_receiver_enabled || (var.proxy_enabled && var.receiver_worker_instance_id != "" && var.receiver_worker_security_group_id != "")
    error_message = "Receiver requires the enabled PgCat fleet and exact ACK worker binding."
  }
}
variable "monitoring_agent_enabled" {
  type    = bool
  default = false
  validation {
    condition     = !var.monitoring_agent_enabled || var.metrics_receiver_enabled
    error_message = "The private receiver must be reviewed before enabling node agents."
  }
}
variable "receiver_worker_instance_id" {
  type    = string
  default = ""
}
variable "receiver_worker_security_group_id" {
  type    = string
  default = ""
}

