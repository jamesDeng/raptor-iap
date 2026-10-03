variable "account_id" {
  type = string
  validation {
    condition     = can(regex("^[0-9]{12,20}$", var.account_id))
    error_message = "Account ID must contain 12–20 digits."
  }
}
variable "region" {
  type    = string
  default = "ap-southeast-1"
  validation {
    condition     = var.region == "ap-southeast-1"
    error_message = "This experiment requires Singapore."
  }
}
variable "zone" {
  type    = string
  default = "ap-southeast-1a"
  validation {
    condition     = var.zone == "ap-southeast-1a"
    error_message = "This experiment requires Singapore zone A."
  }
}
variable "name_prefix" {
  type    = string
  default = "raptor-storage"
  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,31}$", var.name_prefix))
    error_message = "Use a lowercase prefix of at most 32 characters starting with a letter."
  }
}
variable "vpc_cidr" {
  type    = string
  default = "10.60.0.0/16"
  validation {
    condition     = try(cidrnetmask(var.vpc_cidr) == "255.255.0.0" && var.vpc_cidr == "${cidrhost(var.vpc_cidr, 0)}/16", false)
    error_message = "VPC must be a canonical IPv4 /16 network."
  }
}
variable "vswitch_cidr" {
  type    = string
  default = "10.60.1.0/24"
  validation {
    condition     = try(cidrnetmask(var.vswitch_cidr) == "255.255.255.0" && var.vswitch_cidr == "${cidrhost(var.vswitch_cidr, 0)}/24", false)
    error_message = "vSwitch must be a canonical IPv4 /24 network."
  }
  validation {
    condition     = try(join(".", slice(split(".", cidrhost(var.vswitch_cidr, 0)), 0, 2)) == join(".", slice(split(".", cidrhost(var.vpc_cidr, 0)), 0, 2)), true)
    error_message = "vSwitch must be contained in the VPC."
  }
}

variable "filesystem_id" {
  type    = string
  default = null
  validation {
    condition     = var.filesystem_id == null || can(regex("^[A-Za-z0-9-]{1,128}$", var.filesystem_id))
    error_message = "Filesystem ID must be an unqualified alphanumeric/hyphen identifier."
  }
}
variable "access_point_id" {
  type    = string
  default = null
  validation {
    condition     = (var.filesystem_id == null && var.access_point_id == null) || (var.filesystem_id != null && can(regex("^ap-[A-Za-z0-9-]{1,125}$", var.access_point_id)))
    error_message = "Supply both storage IDs or neither; Access Point ID must be an unqualified ap- identifier."
  }
}
