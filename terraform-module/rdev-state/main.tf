data "alicloud_account" "current" {}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Authenticated account does not match state bootstrap account."
    }
  }
}
# Bootstrap separately; never destroy the state store with the environment it records.
resource "alicloud_oss_bucket" "state" {
  bucket        = var.bucket_name
  depends_on    = [terraform_data.account_guard]
  force_destroy = false
  server_side_encryption_rule {
    sse_algorithm = "AES256"
  }
  versioning {
    status = "Enabled"
  }
  lifecycle {
    prevent_destroy = true
  }
}
resource "alicloud_ots_instance" "lock" {
  name          = var.lock_instance_name
  depends_on    = [terraform_data.account_guard]
  instance_type = "HighPerformance"
  description   = "Terraform state locks for raptor-iap"
  tags          = { Project = "raptor-iap", Owner = "rdev-state" }
  lifecycle {
    prevent_destroy = true
  }
}
resource "alicloud_ots_table" "lock" {
  instance_name = alicloud_ots_instance.lock.name
  table_name    = "terraform_lock"
  time_to_live  = -1
  max_version   = 1
  primary_key {
    name = "LockID"
    type = "String"
  }
  lifecycle {
    prevent_destroy = true
  }
}

resource "alicloud_oss_bucket_acl" "state" {
  bucket = alicloud_oss_bucket.state.bucket
  acl    = "private"
}
