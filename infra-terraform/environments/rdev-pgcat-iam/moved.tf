moved {
  from = alicloud_ram_role.config
  to   = module.config_iam.alicloud_ram_role.config
}
moved {
  from = alicloud_ram_policy.config
  to   = module.config_iam.alicloud_ram_policy.config
}
moved {
  from = alicloud_ram_role_policy_attachment.config
  to   = module.config_iam.alicloud_ram_role_policy_attachment.config
}
