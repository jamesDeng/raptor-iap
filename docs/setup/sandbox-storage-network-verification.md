# Storage-network preparation receipt — October 3, 2026

Terraform 1.13.3 and aliyun/alicloud 1.293.0 were installed locally and verified. The darwin_arm64 Terraform archive SHA256 is `8362e7284b38a1194884963deed83481696d468b42dab88052775f4280383584`, matched to the official release checksum manifest. Provider locks include darwin_arm64 and linux_amd64.

The real, read-only network-stage plan succeeded with the existing infra-ops-poc profile. Authenticated account matches the configured account; all Terraform preconditions passed. It proposes 10 cloud creates: one VPC, one zone-A vSwitch, one security group, six rules and one execution role. A local terraform_data guard is the eleventh planned object. No updates/deletes/replacements, NAS policy, attachment, NAT/EIP or compute instance are planned. The five outputs include the four bootstrap fields and nas_policy_attached=false.

Saved plan, JSON inspection, account-specific variables and logs remain under ignored `.raptor-local/poc-sg-storage/` with directory mode 0700 and file mode 0600. No real cloud apply was run. The plan does not prove create permission, effective cloud firewall rules, mounting, managed public egress or Pi persistence.

Offline checks cover 14 module runs, 2 root runs and 24 Python tests, including actual local-only Terraform output JSON serialization into the bootstrap loader. Remote CI has not run for this branch. The Singapore AgenticFS price remains unverified; live storage apply is blocked.

## Lock file hashes

- `terraform-module/sandbox-storage-network/.terraform.lock.hcl`: `2010cf0c01d5e4a707158c9abe969c2d94ba24cf9d6e6d65c1a0653ee67d85cc`
- `infra-terraform/environments/poc-sg-storage/.terraform.lock.hcl`: `2010cf0c01d5e4a707158c9abe969c2d94ba24cf9d6e6d65c1a0653ee67d85cc`
