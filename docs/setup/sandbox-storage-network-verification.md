# Storage-network preparation receipt — October 3, 2026

Terraform 1.13.3 and aliyun/alicloud 1.293.0 were installed locally and verified. The darwin_arm64 Terraform archive SHA256 is `8362e7284b38a1194884963deed83481696d468b42dab88052775f4280383584`, matched to the official release checksum manifest. Provider locks include darwin_arm64 and linux_amd64.

The real, read-only network-stage plan succeeded with the existing infra-ops-poc profile. Authenticated account matches the configured account; the early account check passed. The fresh execution-time account check intentionally remains unknown until apply; all other checks passed. It proposes 10 cloud creates: one VPC, one zone-A vSwitch, one security group, six rules and one execution role. Two local terraform_data records enforce the account boundary. One additional deferred account read is scheduled for execution. No updates/deletes/replacements, NAS policy, attachment, NAT/EIP or compute instance are planned. The five outputs include the four bootstrap fields and nas_policy_attached=false.

Saved plan, JSON inspection, account-specific variables and logs remain under ignored `.raptor-local/poc-sg-storage/` with directory mode 0700 and file mode 0600. No real cloud apply was run. The plan does not prove create permission, effective cloud firewall rules, mounting, managed public egress or Pi persistence.

Offline checks cover 16 module runs, 2 root runs and 25 Python tests, including actual local-only Terraform output JSON serialization into the bootstrap loader. Remote CI has not run for this branch. The Singapore AgenticFS price remains unverified; live storage apply is blocked.

## Lock file hashes

- `terraform-module/sandbox-storage-network/.terraform.lock.hcl`: `2010cf0c01d5e4a707158c9abe969c2d94ba24cf9d6e6d65c1a0653ee67d85cc`
- `infra-terraform/environments/poc-sg-storage/.terraform.lock.hcl`: `2010cf0c01d5e4a707158c9abe969c2d94ba24cf9d6e6d65c1a0653ee67d85cc`

## Final review and regression fixes

One independent reviewer identified two Important issues: saved-plan credential switching could bypass the plan-time account check, and firewall tests did not detect unrestricted egress. The account regression failed on the pre-fix tree, then passed with a fresh account read deferred to apply. A simulated execution identity change rejects the operation before any mocked cloud resource is created. The firewall regression rejected a temporary unrestricted-egress mutation; the intended rule was restored. No second review was dispatched; fixes were verified by the full suites.

The read-only plan was regenerated after those fixes. It still proposes 10 cloud creates, now with two local records and a deferred account read. No real apply was performed. Lifecycle preconditions are not a destroy-time account boundary; future destroy requires an independent authenticated preflight and explicit authorization.

## Execution decisions

# SDD ledger — plan: docs/superpowers/plans/2026-10-03-sandbox-storage-network.md
Execution: inline approved October 3. Baseline 22 Python tests pass.
Ruling: retain the existing feature-branch checkout specified by the approved plan — no separate worktree was requested — cost if wrong: no extra checkout isolation; main remains untouched.
Pre-flight: Task 1 module outputs consumed unchanged by Task 2 root; Task 2 root/module tests consumed by Task 3 workflow. No interface mismatch found.
Runtime: Terraform 1.13.3 darwin_arm64 archive SHA256 8362e7284b38a1194884963deed83481696d468b42dab88052775f4280383584 verified against HashiCorp manifest; local ignored binary only.
Task 1 RED: with pinned provider installed, tests fail because module outputs/resources are undeclared; no cloud calls.
Task 1 complete: commit 1482646, 9 mocked Terraform runs pass; provider hashes recorded for both platforms.
Task 2 RED: permission tests fail on absent policy resources. Local output contract test fails because fixture module is absent.
Ruling: offline provider tests require unrestricted local process/socket startup — restricted runner prevents provider handshake — cost if wrong: tests still must be checked for mocked providers; no credentials or cloud calls are required.
Task 2 complete: 14 module mock runs, 2 root mock runs and 24 Python tests pass; actual local-only Terraform JSON serialization loads through bootstrap.
Ruling: bootstrap-only CI selects its own test file while new Terraform CI runs the full suite — new serialization test requires Terraform — cost if wrong: cross-component regressions depend on the full Terraform workflow.
Task 3 complete: real read-only plan creates 10 cloud objects plus one local guard, all preconditions pass; no mutations performed. fmt/validate, 16 mocked Terraform runs, 24 Python tests and whitespace checks pass. Private plan/input/export ignore and 0700/0600 permissions verified.
Final review: one fresh reviewer found 2 Important issues, no Critical/Minor.
Final: fixed saved-plan identity gap — test_saved_plan_requires_apply_time_identity_read RED→GREEN and changed_identity_before_execution rejects a different apply identity before cloud resources; 25 Python tests pass.
Final: fixed firewall coverage gap — temporary unrestricted-egress mutant rejected by exact six-tuple assertion; original rules restored; 16 module mock runs pass.
Final: Ruling: effective cloud firewall/NFS/OpenAI connectivity and AgenticFS policy scope await live acceptance — offline configuration and read-only planning cannot prove them — cost if wrong: live mounting or traffic may fail.
Final: Ruling: opaque storage IDs do not prove ownership — preserve required authenticated bootstrap inspection before permission apply — cost if wrong: manual validation can be omitted; this module does not independently query storage ownership.
Final: Ruling: pricing and remaining spend are operational gates — no paid storage action is performed — cost if wrong: later operator budget checks must account for delayed bills and retention.
Final: Ruling: OAuth persistence, serialization, NAT alternatives and wider deployment remain separate work — outside the approved network slice — cost if wrong: completing this module alone does not deliver the full POC.
Final: Ruling: destroy-time account enforcement requires a future independent execution preflight — lifecycle preconditions are not a destroy boundary; documentation now states that explicitly — cost if wrong: directly executing a destroy with changed credentials can affect the wrong account.

Final verification: 18 mocked Terraform runs and 25 Python tests pass; formatting, validation and whitespace checks passed. No deferred minor findings.
