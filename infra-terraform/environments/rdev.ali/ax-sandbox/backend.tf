terraform {
  backend "oss" {
    region           = "ap-southeast-1"
    prefix           = "ax-sandbox.ali"
    key              = "terraform.tfstate"
    acl              = "private"
    encrypt          = true
    tablestore_table = "terraform_lock"
  }
}
