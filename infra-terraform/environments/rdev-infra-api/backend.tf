terraform {
  backend "oss" {
    region  = "ap-southeast-1"
    prefix  = "rdev.ali"
    key     = "infra-api.tfstate"
    acl     = "private"
    encrypt = true
  }
}
