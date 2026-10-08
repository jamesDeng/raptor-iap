terraform {
  backend "oss" {
    region           = "ap-southeast-1"
    prefix           = "ecs-ax.ali"
    key              = "terraform.tfstate"
    acl              = "private"
    encrypt          = true
    tablestore_table = "terraform_lock"
  }
}
