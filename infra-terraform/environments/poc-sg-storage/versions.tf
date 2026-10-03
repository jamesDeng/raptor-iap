terraform {
  backend "local" { path = "../../../.raptor-local/poc-sg-storage/terraform.tfstate" }
  required_version = "= 1.13.3"
  required_providers {
    alicloud = { source = "aliyun/alicloud", version = "= 1.293.0" }
  }
}
