variable "vswitch_id" {
  type = string
  validation {
    condition     = var.vswitch_id == "vsw-t4nop9qf6v46gw2sa8l7d"
    error_message = "Only the owned Singapore rdev worker subnet is allowed."
  }
}
variable "publish_dns" {
  type    = bool
  default = false
}
variable "enable_plan_access" {
  type    = bool
  default = false
}
variable "account_id" {
  type    = string
  default = ""
}
variable "state_bucket" {
  type    = string
  default = ""
}
