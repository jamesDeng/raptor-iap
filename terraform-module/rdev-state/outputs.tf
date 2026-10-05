output "bucket" { value = alicloud_oss_bucket.state.bucket }
output "lock_instance" { value = alicloud_ots_instance.lock.name }
output "lock_table" { value = alicloud_ots_table.lock.table_name }
