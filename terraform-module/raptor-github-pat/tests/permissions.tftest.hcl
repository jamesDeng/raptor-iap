mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
}

run "private_pat_store" {
  command = plan
  variables {
    bucket            = "raptor-rdev-github-pat-1360282071200743"
    oidc_provider_arn = "acs:ram::1360282071200743:oidc-provider/ack-rrsa-c92787e953503492ea141a744c81498f1"
    oidc_issuer       = "https://oidc-ack-ap-southeast-1.oss-ap-southeast-1.aliyuncs.com/cluster"
  }
  assert {
    condition     = alicloud_oss_bucket_acl.pat.acl == "private" && alicloud_oss_bucket.pat.server_side_encryption_rule[0].sse_algorithm == "AES256" && alicloud_oss_bucket.pat.force_destroy == false
    error_message = "PAT bucket must be private, encrypted, and protected against destroy."
  }
  assert {
    condition     = strcontains(alicloud_ram_policy.backend.policy_document, "github-pat/*") && !strcontains(alicloud_ram_policy.backend.policy_document, "auth/*")
    error_message = "Backend IAM must stay on the dedicated PAT prefix."
  }
  assert {
    condition     = strcontains(alicloud_ram_role.backend.assume_role_policy_document, "system:serviceaccount:raptor-system:raptor-backend")
    error_message = "Only the backend service account may assume the PAT role."
  }
}
