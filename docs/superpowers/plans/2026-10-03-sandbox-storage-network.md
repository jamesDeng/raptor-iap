# Sandbox storage network Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Deliver validated Terraform networking/RAM configuration and a saved, reviewed Singapore network-stage plan without applying resources.

**Architecture:** A reusable module owns the dedicated network, execution role and optional NAS policy; a concrete environment root supplies the Singapore configuration. Storage IDs activate a second permission stage after the SDK bootstrap runs. Offline tests mock the provider; an account-aware plan is generated separately with operator credentials.

**Tech Stack:** Terraform 1.13.3, `aliyun/alicloud` 1.293.0, Terraform mock-provider tests, GitHub Actions, existing Python bootstrap tests.

**Spec:** `docs/superpowers/specs/2026-10-03-sandbox-storage-network-design.md` (user approved October 3).

## Global Constraints

- One public `jamesDeng/raptor-iap` repository; account-specific inputs and Terraform state/plans remain ignored locally.
- Only region `ap-southeast-1`, zone `ap-southeast-1a`; default VPC `10.60.0.0/16`, vSwitch `10.60.1.0/24`.
- Terraform owns network/RAM; the approved SDK exception owns AgenticFS filesystem, AgenticSpace, Access Point and Sandbox Volume.
- Execution role trusts `fc.aliyuncs.com` through `sts:AssumeRole`; no administrator, NAS control-plane or Volume permissions.
- NAS data actions: `nas:ClientMount`, `nas:ClientWrite`, `nas:ClientRootAccess`; exact filesystem resource and Access Point ARN condition, never wildcard interim access.
- No added inbound allow rules, SSH, NAT/EIP, compute instances, ACK, RDS or load balancer in this slice.
- Network-only stage has no NAS data policy. Both storage IDs are required to enable the permission stage.
- Outputs consumed by bootstrap: `vpc_id`, `vswitch_id`, `security_group_id`, `execution_role_arn`.
- No live apply/destroy, storage creation, sandbox launch, real OAuth persistence, push or merge during implementation. A read-only account-aware Terraform plan is within scope.
- Total Aliyun POC budget below RMB1,000; actual Singapore storage price remains unverified. No manufactured pricing evidence.

## Review Focus

1. Caller account differs from input: stop before every cloud mutation, including an otherwise valid permission-stage plan.
2. CIDRs are syntactically valid but noncanonical, IPv6, wrong size or outside the VPC: reject rather than normalize into an unintended network.
3. Partial/malformed storage identifiers: no partial or wildcard authorization; reject invalid configurations.
4. Implicit security-group defaults and same-group access: express deny baselines and internal isolation, without claiming an offline test proves effective cloud behavior.
5. Mock output formats hide a bootstrap incompatibility: round-trip realistic `terraform output -json` values through the actual Python loader.

## File map and installation

Create module `terraform-module/sandbox-storage-network/{versions,variables,main,outputs}.tf`, `tests/network.tftest.hcl` and `tests/permissions.tftest.hcl` within that module. Create root `infra-terraform/environments/poc-sg-storage/{versions,variables,main,outputs}.tf`, its `.terraform.lock.hcl`, and `tests/bootstrap_contract.tftest.hcl`. Create `tests/test_terraform_bootstrap_contract.py`, `tests/fixtures/terraform-bootstrap-contract/main.tf`, `docs/setup/sandbox-storage-network.md` and `.github/workflows/sandbox-storage-network.yml`; modify `.gitignore` and root `README.md`.

Before Task 1 tests, obtain Terraform 1.13.3 for the host architecture into ignored `.raptor-local/bin/`, verify the archive against HashiCorp's release checksum manifest and record the checksum. Do not replace a system runtime or install a different version silently. Initial provider download is necessary to load its real schema even with mocked API calls. Pin `aliyun/alicloud = 1.293.0`; commit root and module lock files with hashes for `darwin_arm64` and `linux_amd64`. Runtime/network download failure is a preparation blocker, not a failed cloud feasibility test.

Use existing feature branch `feat/sandbox-storage-network`; main is untouched. Preserve inline execution as selected for the preceding slice unless the user changes it at this plan review. One final independent review follows implementation; per-task implementation remains in this session.

## Task 1: Network module and blocking account validation

**Files:** module versions/variables/main/outputs, `tests/network.tftest.hcl`, `.gitignore`.

**Interfaces:** module inputs `account_id: string`, `region: string`, `zone: string`, `name_prefix: string`, `vpc_cidr: string`, `vswitch_cidr: string`; outputs `vpc_id: string`, `vswitch_id: string`, `security_group_id: string`, `execution_role_arn: string`.

Implementation choices: require IPv4 VPC /16 and vSwitch /24 for this small module. This makes containment unambiguous: canonical base addresses, matching first two IPv4 octets, and the specified mask lengths. Defaults are the approved CIDRs. Require a 12–20 digit account ID and lowercase prefix matching `^[a-z][a-z0-9-]{0,31}$`.

- [x] Write mock-provider runs `network_only`, `wrong_account`, `wrong_region`, `wrong_zone`, `invalid_cidrs`, `outside_vpc`, `noncanonical_cidr`, `ipv6_rejected`. Override `alicloud_account.current.id` with synthetic account `1234567890123456`. Assert four outputs, expected CIDRs, no inbound accept rules, `inner_access_policy == "Drop"`, exact role trust and `force == false`. Invalid cases use `expect_failures` targeting their variable validation or blocking account guard; include valid-looking caller mismatch `9999999999999999`.
- [x] Run `terraform -chdir=terraform-module/sandbox-storage-network init -backend=false` and `terraform -chdir=terraform-module/sandbox-storage-network test`; confirm RED from missing module resources/variables before implementing them. Tests must be fully mocked; never fall back to the real provider if mocks are misconfigured.
- [x] Implement native `alicloud_vpc`, `alicloud_vswitch`, `alicloud_security_group`, `alicloud_security_group_rule`, `alicloud_ram_role`. Read `data.alicloud_account.current.id`; use a blocking `terraform_data.account_guard` lifecycle precondition, not a warning-only `check`. VPC and execution role depend on this guard; all other cloud resources depend transitively on them. Use current fields `role_name`, `assume_role_policy_document`, `security_group_name`, `inner_access_policy = "Drop"`; role `force = false`.
- [x] Define security-group deny-all IPv4 ingress and egress baselines at priority 100. Four accept egress rules at priority 1: TCP2049 to VPC CIDR, TCP443 to `0.0.0.0/0`, UDP53 and TCP53 to `0.0.0.0/0`. Explicit rules may override cloud defaults by priority; review actual defaults during later inspection. Name resources deterministically from prefix and tag supported resources with project `raptor-iap`, environment `poc-sg-storage`.
- [x] Run module formatting, validation and tests. Expected: all mock tests pass without credentials; invalid inputs fail at their expected boundary. Record hashes in generated lock files.
- [x] Commit module, tests and ignore rules. Ignore `*.tfvars`, `*.tfvars.json`, `*.tfplan`, plan/output JSON, state/backups and tool binaries; do not ignore lock files or `.tftest.hcl`.

## Task 2: Scoped permission stage and environment/bootstrap contract

**Files:** module variables/main/outputs and `tests/permissions.tftest.hcl`; root versions/variables/main/outputs and `tests/bootstrap_contract.tftest.hcl`; Python contract test and local-only fixture module.

**Interfaces:** Task 1 inputs/outputs unchanged. Add paired optional `filesystem_id: string|null`, `access_point_id: string|null`, defaults null; expose `nas_policy_attached: bool`. Root forwards module inputs and named outputs unchanged. Readiness/ownership inspection remains the existing SDK tool's responsibility; IDs alone cannot prove account ownership.

- [x] Write runs `no_storage_policy`, `scoped_storage_policy`, `partial_ids_rejected`, `malformed_ids_rejected`, `permission_stage_wrong_account`. Use synthetic IDs `fs-fixture`, `ap-fixture`. Assert null pair yields zero policy/attachment resources; valid pair yields exactly one custom policy/attachment. Decode policy JSON and assert the three-action set, exact resource `acs:nas:ap-southeast-1:1234567890123456:filesystem/fs-fixture`, and exact StringEquals `nas:AccessPointArn` value `acs:nas:ap-southeast-1:1234567890123456:accesspoint/ap-fixture`; no `*` anywhere in data-policy values. Partial pairs and path/ARN/wildcard strings fail validation. Account mismatch remains blocked.
- [x] Run tests and observe RED for absent permission-stage interfaces/resources.
- [x] Implement paired validation, restricted ID syntax (`filesystem_id`: alphanumeric/hyphen, 1–128 chars; `access_point_id`: `ap-` followed by alphanumeric/hyphen, total at most 128 chars), conditional `alicloud_ram_policy` and `alicloud_ram_role_policy_attachment`, policy type `Custom`, `force = false`. Build JSON with `jsonencode`; use current provider fields `policy_name`/`policy_document`. Do not add hard-coded actual account IDs or generated SDK calls to Terraform.
- [x] Implement the concrete root with provider region, module source `../../../terraform-module/sandbox-storage-network` (three parent traversals reach the repository root). Root provider uses the supported operator credential chain; verify official provider configuration before choosing the local credential source. No access keys in HCL or arguments. Root defaults match Task 1, account remains required, storage IDs default null.
- [x] Add root mock-provider output overrides using realistic synthetic IDs and a role ARN for the synthetic account; assert the four root output names/values. Add Python `test_terraform_outputs_load_into_bootstrap`: copy the fixture module into a temporary directory, initialize without a backend and apply only its built-in `terraform_data` resource containing synthetic network values, then capture actual `terraform output -json` and feed it into `load_config`; assert all four fields match. The fixture has no external provider and exports the same four output names as the root. Assert missing named output is rejected. This verifies actual Terraform JSON serialization plus the loader contract; root output semantics are checked by the mock test. No real alicloud provider apply is permitted.
- [x] Run root/module fmt, init/validate/tests and the existing Python suite. Expected: both policy stages pass and all invalid configurations fail at named boundaries; existing 22 bootstrap tests remain green. Pin provider hashes for both platform targets.
- [x] Commit scoped policy, root configuration and interface tests.

## Task 3: Offline CI, operator guide and account-aware plan

**Files:** `.github/workflows/sandbox-storage-network.yml`, `docs/setup/sandbox-storage-network.md`, root README; ignored local plan/evidence under `.raptor-local/`.

**Interfaces:** CI consumes Tasks 1/2 directories and test commands. Operator exports four root outputs into existing bootstrap config; storage IDs come only from its authenticated nonsecret inventory/inspection. This task adds no deploy command wrapper or automatic apply gate bypass.

- [x] Add a workflow for pull requests/main changes to the Terraform directories, contract test and workflow. Use ubuntu-24.04, contents:read, no secrets, no id-token permission and checkout with persist-credentials:false. Pin checkout/setup-terraform to reviewed upstream commit SHAs, set Terraform 1.13.3 and disable its output wrapper. Use init with `-backend=false -lockfile=readonly`, fmt/check, validate and mocked tests in both directories, plus pinned Python SDK installation and full Python suite. Expected on a clean runner: no credentials required and no cloud API calls.
- [x] Write the operator guide: install/checksum/runtime pin; credentials from verified provider-supported source; network-only and permission-stage plan commands; ignored local state/plans with 0700 directory/0600 files and restrictive umask; outputs to bootstrap; pricing/remaining-budget gate; no wildcard widening; no shared-role modifications; later synthetic checks and SDK-before-Terraform teardown. State explicitly that lock files are committed and state/plans are not.
- [x] Verify ignore behavior with synthetic state/plan/variable/output files in a temporary ignored directory; no actual secret fixture. Read the final diff and scan tracked added files for credential-shaped fixtures. Expected: generated private artifacts ignored, source/lock/tests tracked, no keys/tokens or live account values.
- [x] Using the existing operator credential source, run an account-aware **network-only** `terraform plan -out=<ignored absolute path> -input=false`, with both storage IDs null and explicit account validation. Inspect its locally saved JSON for expected resource types/actions, no deletes/replacements, four network/role outputs and no NAS policy. If permissions or provider authentication block this, preserve the error code without secrets, stop the real-plan step and report it; do not mark mock tests as satisfying this acceptance criterion. Never run `terraform apply`.
- [x] Save a nonsecret review receipt: Terraform/provider versions, lock hashes, expected create counts/types, account-match result, no permission policy in stage one, no provisioned resources, and unresolved Singapore storage quote. Keep account-specific plan data ignored. Confirm the plan's effective assumptions against provider schema; implicit cloud rule behavior is still a live acceptance item.
- [ ] Run final offline suite and whitespace checks. Commit workflow, guide, README and nonsecret verification receipt. Obtain one fresh independent whole-branch review, fix important findings with regressions, and present integration options. Remote CI is not claimed passed until a later authorized push runs it.

## Later live handoff (not executed by this plan)

Review real network plan and current charges/price, then obtain authorization for apply. After Terraform network stage, populate bootstrap outputs and run its gated storage setup. Verify actual storage IDs/account/region before a second Terraform permission plan. Then run synthetic A/B storage and model connectivity checks with bounded sandbox lifetimes and cleanup; real OAuth persistence follows separately. No resource teardown that destroys data is implied by implementing this plan.

## References and plan self-review

- Pinned official provider docs/source: https://github.com/aliyun/terraform-provider-alicloud/tree/v1.293.0/website/docs
- Terraform mock behavior: https://developer.hashicorp.com/terraform/language/tests/mocking (available from 1.7; mock apply affects test state, not cloud APIs).
- Runtime checksum manifest: https://releases.hashicorp.com/terraform/1.13.3/terraform_1.13.3_SHA256SUMS (read-only availability verified during planning).
- Sources for RAM scope, network prerequisites and pricing are in the approved spec.

Self-review: three tasks cover module/account/network, staged policy/root contract, then CI/docs/real-plan receipt. All five review-focus cases have owning tests. Runtime/provider pins and field names were checked against official sources; real authentication and effective cloud networking remain execution/live gates. The plan requires no paid resources and does not claim a verified Singapore storage quote. Execution method remains inline unless changed by the user.
