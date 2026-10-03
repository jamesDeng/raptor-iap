output "vpc_id" { value = module.storage_network.vpc_id }
output "vswitch_id" { value = module.storage_network.vswitch_id }
output "security_group_id" { value = module.storage_network.security_group_id }
output "execution_role_arn" { value = module.storage_network.execution_role_arn }
output "nas_policy_attached" { value = module.storage_network.nas_policy_attached }
