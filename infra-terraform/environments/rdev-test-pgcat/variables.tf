variable "enabled" {
  type    = bool
  default = false
}
variable "stack" {
  type = object({
    account_id              = string
    env_code                = string
    vpc_id                  = string
    db_code                 = string
    db_zone                 = string
    db_vswitch_id           = string
    db_class                = string
    db_engine_version       = string
    db_storage_gib          = number
    db_client_cidrs         = list(string)
    proxy_code              = string
    proxy_vswitch_ids       = list(string)
    nlb_zones               = list(object({ zone_id = string, vswitch_id = string }))
    proxy_security_group_id = string
    proxy_image_id          = optional(string, "ubuntu_24_04_x64_20G_alibase_20260916.vhd")
    proxy_container_image   = string
    proxy_instance_class    = string
    target_database         = string
    secret_reference        = string
    execution_role_name     = string
    bootstrap_revision      = string
    bootstrap_reviewed      = bool
  })
  default = null
  validation {
    condition     = !var.enabled || var.stack != null
    error_message = "Enabled instances require concrete reviewed configuration."
  }
}
