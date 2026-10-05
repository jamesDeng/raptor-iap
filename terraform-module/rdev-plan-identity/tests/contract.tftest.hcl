mock_provider "alicloud" {
  mock_data "alicloud_account" { defaults = { id = "1234567890123456" } }
}
run "restricted_planning_identity" {
  command = plan
  override_resource {
    target          = alicloud_ims_oidc_provider.github
    override_during = plan
    values          = { arn = "acs:ram::1234567890123456:oidc-provider/raptor-iap-github" }
  }
  variables {
    account_id   = "1234567890123456"
    fingerprints = ["1111111111111111111111111111111111111111"]
  }
  assert {
    condition     = jsondecode(alicloud_ram_role.plan.assume_role_policy_document).Statement[0].Condition.StringEquals["oidc:sub"] == "repo:jamesDeng/raptor-iap:environment:rdev.ali-plan"
    error_message = "Trust must bind the exact repo environment."
  }
  assert {
    condition     = alltrue([for s in jsondecode(alicloud_ram_policy.plan.policy_document).Statement : alltrue([for a in s.Action : can(regex(":(Describe|Get|List)", a))])])
    error_message = "Planning role must not contain cloud mutation permissions."
  }
  assert {
    condition     = anytrue([for s in jsondecode(alicloud_ram_policy.plan.policy_document).Statement : s.Effect == "Deny" && contains(s.Action, "cs:DescribeClusterUserKubeconfig") && contains(s.Action, "cs:DescribeClusterV2UserKubeconfig") && contains(s.Action, "cs:DescribeClusterAttachScripts") && contains(s.Action, "cs:GetKubernetesTrigger")])
    error_message = "Planning role must explicitly deny kubeconfig credential retrieval."
  }

}
