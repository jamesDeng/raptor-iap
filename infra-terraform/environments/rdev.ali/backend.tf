terraform {
  # Bucket and Tablestore identifiers are supplied with a private backend config.
  # Credentials must come from the execution environment, never this block.
  backend "oss" {
    region  = "ap-southeast-1"
    prefix  = "rdev.ali"
    key     = "terraform.tfstate"
    acl     = "private"
    encrypt = true
  }
}
