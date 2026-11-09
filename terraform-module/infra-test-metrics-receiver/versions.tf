terraform {
  required_version = ">= 1.13.0, < 1.14.0"
  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "= 1.293.0"
    }
  }
}
