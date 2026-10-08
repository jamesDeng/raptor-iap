variable "account_id" {
  type = string
  validation {
    condition     = can(regex("^[0-9]{12,20}$", var.account_id))
    error_message = "Concrete account ID required."
  }
}
variable "role_name" {
  type = string
  validation {
    condition     = can(regex("^[A-Za-z0-9-]{1,64}$", var.role_name))
    error_message = "Concrete RAM role name required."
  }
}
variable "bucket_name" {
  type = string
  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$", var.bucket_name))
    error_message = "One concrete OSS bucket required."
  }
}
variable "object_key" {
  type = string
  validation {
    condition     = length(var.object_key) > 0 && !startswith(var.object_key, "/") && length(regexall("[?*]", var.object_key)) == 0
    error_message = "One concrete object key required; policy wildcards forbidden."
  }
}
