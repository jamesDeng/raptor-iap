# Infra API execution permissions

With `enable_proxy_commands = true`, the existing function role's policy includes two resolver reads:

- `nlb:ListServerGroups` on sorted, distinct exact server-group ARNs derived from `proxy_targets`, with both `serverGroup/<id>` and `servergroup/<id>` aliases, matching the existing `ListServerGroupServers` compatibility scope. Both aliases use the same configured IDs; no wildcard or additional target is introduced. Alibaba documents the lowercase shape in the [ListServerGroups RAM authorization table](https://www.alibabacloud.com/help/en/slb/network-load-balancer/developer-reference/api-nlb-2022-04-30-listservergroups).
- `nlb:ListListeners` on `acs:nlb:<region>:<account>:listener/*`, the required resource shape in the [ListListeners RAM authorization table](https://www.alibabacloud.com/help/en/slb/network-load-balancer/developer-reference/api-nlb-2022-04-30-listlisteners). This expands discovery reads across listeners in the selected account and region. The resolver's related-load-balancer request filter is not an IAM condition or exact-listener permission boundary; the table lists no service-specific condition key.

Both reads are omitted when proxy commands are disabled. Existing mutation actions, resources, role trust, attachments and Terraform resource addresses are unchanged.

The 2026-10-31 qualification recorded `ListServerGroups` returning `Forbidden.NoPermission` for the exact configured server group while policy v4 granted only its lowercase ARN. Adding the exact-ID case alias is a compatibility hypothesis; the failure's cause is not proven. Mock tests verify the generated aliases, while live authorization remains unverified until a read succeeds after the reviewed Terraform apply.

This case-alias correction is unapplied. Mock-provider contract tests verify generated policy only; they are not a remote Terraform plan or live authorization check. Before any grant, merge or apply, review the concrete Terraform plan and its sanitized PR comment, obtain the required owner authorization, and use the existing reviewed GitOps apply flow. Expect only the intended in-place policy update plus existing identity-refresh guard/data-source reads; investigate any unrelated change. No manual grant or borrowed operator role is part of this proposal.
