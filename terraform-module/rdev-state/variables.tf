variable "bucket_name" {
  type        = string
  description = "Globally unique state bucket name; supplied through private provisioning inputs."
}
variable "lock_instance_name" {
  type = string
}

variable "account_id" { type = string }
