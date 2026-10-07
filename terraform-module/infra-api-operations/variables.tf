variable "region" {
  type    = string
  default = "ap-southeast-1"
  validation {
    condition     = var.region == "ap-southeast-1"
    error_message = "Only Singapore is allowed."
  }
}
variable "account_id" { type = string }
variable "name" { type = string }
variable "group_id" {
  type        = string
  description = "Existing reviewed API Gateway group; this module creates no group or gateway instance."
}
variable "function_name" { type = string }
variable "trigger_url" {
  type = string
  validation {
    condition     = can(regex("^https://[a-zA-Z0-9.-]+(:[0-9]+)?(/[^?#]*)?$", var.trigger_url))
    error_message = "Protected FC3 HTTPS trigger URL without credentials/query/fragment required."
  }
}
variable "invoke_role_arn" {
  type        = string
  description = "Existing reviewed API Gateway role scoped to invocation of the selected FC function; no RAM grants are created."
  validation {
    condition     = startswith(var.invoke_role_arn, "acs:ram::${var.account_id}:role/")
    error_message = "Invocation role must belong to the selected account."
  }
}
