provider "alicloud" { region = "ap-southeast-1" }
module "raptor_github_pat" {
  source            = "../../../../terraform-module/raptor-github-pat"
  bucket            = "raptor-rdev-github-pat-1360282071200743"
  oidc_provider_arn = var.oidc_provider_arn
  oidc_issuer       = var.oidc_issuer
}
