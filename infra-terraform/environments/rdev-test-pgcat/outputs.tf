output "test_database" { value = var.enabled ? module.database[0].association : null }
output "test_proxy" { value = var.enabled ? module.proxy[0].association : null }
