# Temporary bootstrap entrypoint. Resource ownership belongs to the PgCat module.
provider "alicloud" { region = "ap-southeast-1" }
module "config_iam" {
  source      = "../../../terraform-module/pgcat-ess/modules/config-iam"
  account_id  = "1360282071200743"
  role_name   = "raptor-iap-rdev-pgcat-config"
  bucket_name = "raptor-iap-rdev-pgcat-config-1360282071200743"
  object_key  = "rdev.ali/pgcat-test/config.json"
}
