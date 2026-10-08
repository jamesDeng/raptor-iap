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
variable "proxy_code" {
  type = string
  validation {
    condition     = can(regex("^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$", var.proxy_code))
    error_message = "Generated proxy code required."
  }
}
variable "target_db_code" {
  type = string
  validation {
    condition     = can(regex("^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$", var.target_db_code))
    error_message = "Generated target database code required."
  }
}
variable "target_db_env" {
  type = string
  validation {
    condition     = var.target_db_env == var.env_code
    error_message = "Target DB must belong to the same environment."
  }
}
variable "target_db_host" { type = string }
variable "target_database" { type = string }
variable "vswitch_ids" { type = list(string) }
variable "nlb_zones" {
  type = list(object({ zone_id = string, vswitch_id = string }))
  validation {
    condition     = length(var.nlb_zones) >= 2 && length(distinct([for z in var.nlb_zones : z.zone_id])) == length(var.nlb_zones) && alltrue([for z in var.nlb_zones : startswith(z.zone_id, "ap-southeast-1")])
    error_message = "At least two distinct verified Singapore NLB zones required."
  }
}
variable "security_group_id" { type = string }
variable "image_id" { type = string }
variable "instance_class" { type = string }
variable "secret_reference" { type = string }
variable "execution_role_name" { type = string }
variable "bootstrap_revision" {
  type = string
  validation {
    condition     = can(regex("^[a-f0-9]{40}$", var.bootstrap_revision))
    error_message = "Reviewed bootstrap source commit required."
  }
}
variable "bootstrap_reviewed" {
  type    = bool
  default = false
  validation {
    condition     = var.bootstrap_reviewed
    error_message = "Secret delivery and immutable bootstrap image must be reviewed before this module can plan."
  }
}
