mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1360282071200743" } }
}
variables {
  team_id             = "team"
  volume_id           = "volume"
  bucket              = "raptor-private"
  gitops_state_bucket = "raptor-iap-tfstate-sg-200743"
  gitops_plan_role    = "raptor-iap-rdev-plan"
  gitops_apply_role   = "raptor-iap-rdev-apply"
}
run "plan_identity_cannot_provision_or_write_state" {
  command = plan
  assert {
    condition     = alltrue([for s in jsondecode(alicloud_ram_policy.gitops["plan"].policy_document).Statement : alltrue([for a in s.Action : !contains(["ram:CreateRole", "ram:CreatePolicy", "ram:AttachPolicyToRole", "oss:PutObject"], a)])])
    error_message = "Plan role must remain read-only apart from shared Terraform locking."
  }
  assert {
    condition     = contains(flatten([for s in jsondecode(alicloud_ram_policy.gitops["plan"].policy_document).Statement : s.Resource]), "acs:oss:*:1360282071200743:raptor-iap-tfstate-sg-200743/gateway.ali/terraform.tfstate")
    error_message = "State read must target the existing Gateway state address."
  }
}
run "apply_identity_has_no_delete_or_wildcard_iam" {
  command = plan
  assert {
    condition     = alltrue([for s in jsondecode(alicloud_ram_policy.gitops["apply"].policy_document).Statement : alltrue([for a in s.Action : !contains(["ram:DeleteRole", "ram:DeletePolicy", "ram:DetachPolicyFromRole", "ram:*"], a)])]) && alltrue([for s in jsondecode(alicloud_ram_policy.gitops["apply"].policy_document).Statement : !contains(s.Resource, "acs:ram:*:1360282071200743:role/*") && !contains(s.Resource, "acs:ram:*:1360282071200743:policy/*")])
    error_message = "Apply supplements must use concrete owned identities and no deletion."
  }
}
run "pat_state_bootstrap_is_exact_and_read_only_for_plan" {
  command = plan
  assert {
    condition     = contains(flatten([for s in jsondecode(alicloud_ram_policy.gitops["plan"].policy_document).Statement : s.Resource]), "acs:oss:*:1360282071200743:raptor-iap-tfstate-sg-200743/raptor-github-pat.ali/terraform.tfstate") && alltrue([for s in jsondecode(alicloud_ram_policy.gitops["plan"].policy_document).Statement : !contains(s.Action, "oss:PutBucket") && !contains(s.Action, "oss:PutObject")])
    error_message = "PAT state bootstrap must grant exact read access to the plan role."
  }
  assert {
    condition     = length([for s in jsondecode(alicloud_ram_policy.gitops["plan"].policy_document).Statement : s if contains(s.Action, "oss:ListObjects") && try(s.Condition.StringLike["oss:Prefix"], []) == ["raptor-github-pat.ali/*"]]) == 1
    error_message = "PAT state listing must stay within its prefix."
  }
}
run "pat_apply_bootstrap_has_exact_bucket_and_iam" {
  command = plan
  assert {
    condition     = contains(flatten([for s in jsondecode(alicloud_ram_policy.gitops["apply"].policy_document).Statement : s.Resource]), "acs:oss:*:1360282071200743:raptor-rdev-github-pat-1360282071200743") && contains(flatten([for s in jsondecode(alicloud_ram_policy.gitops["apply"].policy_document).Statement : s.Resource]), "acs:ram:*:1360282071200743:role/raptor-rdev-github-pat-backend") && contains(flatten([for s in jsondecode(alicloud_ram_policy.gitops["apply"].policy_document).Statement : s.Resource]), "acs:ram:*:1360282071200743:policy/raptor-rdev-github-pat-backend")
    error_message = "PAT apply bootstrap must name only the dedicated bucket, role, and policy."
  }
}
