output "resource_id" { value = alicloud_db_instance.test.id }
output "private_endpoint" { value = alicloud_db_instance.test.connection_string }
output "association" { value = { env = var.env_code, db-code = var.db_code } }
