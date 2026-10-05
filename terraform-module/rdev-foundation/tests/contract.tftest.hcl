mock_provider "alicloud" {
  mock_data "alicloud_ram_roles" {
    defaults = { names = ["AliyunCSDefaultRole", "AliyunCSManagedKubernetesRole", "AliyunCSManagedNetworkRole", "AliyunCSManagedCsiRole", "AliyunCSManagedCsiProvisionerRole", "AliyunCSManagedCsiPluginRole", "AliyunServiceRoleForRdsPgsqlOnEcs"] }
  }
  mock_data "alicloud_account" {
    defaults = { id = "1234567890123456" }
  }
}
run "bounded_foundation" {
  command = plan
  variables {
    account_id         = "1234567890123456"
    kubernetes_version = "1.34.1-aliyun.1"
  }
  assert {
    condition     = alicloud_cs_managed_kubernetes.cluster.cluster_spec == "ack.standard" && alicloud_cs_managed_kubernetes.cluster.slb_internet_enabled == false
    error_message = "Cluster must be Basic with private API access."
  }
  assert {
    condition     = alicloud_cs_managed_kubernetes.cluster.skip_set_certificate_authority == true
    error_message = "Environment state must not retain ACK client credentials."
  }
  assert {
    condition     = alicloud_cs_kubernetes_node_pool.platform.desired_size == "1" && length(alicloud_cs_kubernetes_node_pool.platform.instance_types) == 1 && contains(alicloud_cs_kubernetes_node_pool.platform.instance_types, "ecs.e-c1m2.xlarge")
    error_message = "One approved four-core worker only."
  }
  assert {
    condition     = alicloud_db_instance.platform.instance_storage == 10 && alicloud_db_instance.platform.instance_type == "pg.n2e.1c.1m" && alicloud_db_instance.platform.storage_auto_scale == "Disable"
    error_message = "Database must match the reviewed small configuration."
  }
}
run "missing_service_roles" {
  command = plan
  variables {
    account_id         = "1234567890123456"
    kubernetes_version = "1.34.10-aliyun.1"
  }
  override_data {
    target = data.alicloud_ram_roles.prerequisites
    values = { names = [] }
  }
  expect_failures = [terraform_data.service_role_guard]
}
