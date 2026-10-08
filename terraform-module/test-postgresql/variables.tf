variable "account_id" { type = string }
variable "region" {
  type    = string
  default = "ap-southeast-1"
  validation {
    condition     = var.region == "ap-southeast-1"
    error_message = "Only Singapore is supported."
  }
}
variable "env_code" {
  type = string
  validation {
    condition     = can(regex("^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$", var.env_code))
    error_message = "A generated environment code is required."
  }
}
variable "vpc_id" { type = string }
variable "db_code" {
  type = string
  validation {
    condition     = can(regex("^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$", var.db_code))
    error_message = "A generated database code is required."
  }
}
variable "zone" {
  type = string
  validation {
    condition     = startswith(var.zone, "ap-southeast-1")
    error_message = "A verified Singapore zone is required."
  }
}
variable "vswitch_id" { type = string }
variable "instance_class" { type = string }
variable "engine_version" { type = string }
variable "storage_gib" {
  type = number
  validation {
    condition     = var.storage_gib >= 10 && var.storage_gib <= 100 && floor(var.storage_gib) == var.storage_gib
    error_message = "Reviewed integral storage between 10 and 100 GiB required."
  }
}
variable "client_cidrs" {
  type = list(string)
  validation {
    condition     = length(var.client_cidrs) > 0 && alltrue([for c in var.client_cidrs : can(cidrhost(c, 0)) && (startswith(c, "10.") || startswith(c, "192.168.") || can(regex("^172\\.(1[6-9]|2[0-9]|3[01])\\.", c))) && try(tonumber(split("/", c)[1]) >= 16, false)])
    error_message = "Explicit bounded RFC1918 IPv4 client CIDRs required."
  }
}
