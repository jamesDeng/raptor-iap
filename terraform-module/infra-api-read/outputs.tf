output "read_role_arn" { value = alicloud_ram_role.read.arn }
output "invoke_role_arn" { value = alicloud_ram_role.invoke.arn }
output "group_id" { value = alicloud_api_gateway_group.read.id }
# A group subdomain is not proof of configured TLS. Bind an owned domain and
# certificate before configuring Raptor or running the HTTPS acceptance probe.
output "gateway_subdomain" { value = alicloud_api_gateway_group.read.sub_domain }
output "api_ids" { value = { for k, api in alicloud_api_gateway_api.read : k => api.id } }
