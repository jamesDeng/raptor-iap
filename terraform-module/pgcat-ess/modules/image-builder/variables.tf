variable "account_id" { type = string }
variable "vpc_id" { type = string }
variable "vswitch_id" { type = string }

variable "running" {
  type        = bool
  default     = false
  description = "Explicit builder work window; false stops in economical mode while preserving its disk."
}
