# Ownership handoff: apply this root before importing IAM into the live fleet.
# This removes state ownership only. The real role/policy/attachment remain intact.
provider "alicloud" { region = "ap-southeast-1" }
removed {
  from = module.config_iam
  lifecycle { destroy = false }
}
