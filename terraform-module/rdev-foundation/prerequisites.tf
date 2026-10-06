# Account service roles are authorized separately and are never POC teardown resources.
locals {
  required_service_roles = toset(["AliyunCSDefaultRole", "AliyunCSManagedKubernetesRole", "AliyunCSManagedNetworkRole", "AliyunCSManagedCsiRole", "AliyunCSManagedCsiProvisionerRole", "AliyunCSManagedCsiPluginRole", "AliyunServiceRoleForRdsPgsqlOnEcs", "AliyunServiceRoleForNatgw", "AliyunCSManagedLogRole", "AliyunCSManagedCmsRole", "AliyunCSServerlessKubernetesRole", "AliyunCSKubernetesAuditRole", "AliyunCSManagedArmsRole", "AliyunCISDefaultRole", "AliyunOOSLifecycleHook4CSRole"])
}
data "alicloud_ram_roles" "prerequisites" {
  name_regex = "^(${join("|", sort(tolist(local.required_service_roles)))})$"
}
resource "terraform_data" "service_role_guard" {
  lifecycle {
    precondition {
      condition     = length(setsubtract(local.required_service_roles, toset(data.alicloud_ram_roles.prerequisites.names))) == 0
      error_message = "Required ACK/PostgreSQL/NAT service roles are missing. Authorize documented roles before provisioning."
    }
  }
}
