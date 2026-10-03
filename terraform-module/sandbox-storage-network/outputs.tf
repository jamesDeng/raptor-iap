output "vpc_id" { value = alicloud_vpc.network.id }
output "vswitch_id" { value = alicloud_vswitch.sandbox.id }
output "security_group_id" { value = alicloud_security_group.sandbox.id }
output "execution_role_arn" { value = alicloud_ram_role.execution.arn }
