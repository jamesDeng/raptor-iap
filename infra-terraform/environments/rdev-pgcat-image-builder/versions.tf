terraform {
  required_version = "= 1.13.3"
  required_providers {
    alicloud = { source = "aliyun/alicloud", version = "= 1.293.0" }
  }
  backend "oss" {
    bucket              = "raptor-iap-tfstate-sg-200743"
    prefix              = "rdev.ali/pgcat-image-builder"
    key                 = "terraform.tfstate"
    region              = "ap-southeast-1"
    tablestore_endpoint = "https://raptor-tf-lock.ap-southeast-1.ots.aliyuncs.com"
    tablestore_table    = "terraform_lock"
  }
}
