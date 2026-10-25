provider "alicloud" { region = "ap-southeast-1" }
removed {
  from = module.sandbox
  lifecycle {
    destroy = true
  }
}
