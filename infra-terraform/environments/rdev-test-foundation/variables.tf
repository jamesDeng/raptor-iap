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
