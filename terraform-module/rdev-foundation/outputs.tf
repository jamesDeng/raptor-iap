output "cluster_id" {
  value = alicloud_cs_managed_kubernetes.cluster.id
}
output "database_id" {
  value = alicloud_db_instance.platform.id
}
output "database_endpoint" {
  value = alicloud_db_instance.platform.connection_string
}
output "vpc_id" {
  value = alicloud_vpc.env.id
}
