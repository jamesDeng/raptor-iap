variable "ssh_public_key" { type = string }
variable "admin_cidr" {
  type = string
  validation {
    condition     = can(cidrhost(var.admin_cidr, 0)) && can(regex("/32$", var.admin_cidr))
    error_message = "Public SSH requires a single administrator IPv4 /32."
  }
}
