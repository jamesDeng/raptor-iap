output "cluster_id" { value = alicloud_cs_managed_kubernetes.cluster.id }
output "worker_subnet_id" { value = alicloud_vswitch.workers.id }
