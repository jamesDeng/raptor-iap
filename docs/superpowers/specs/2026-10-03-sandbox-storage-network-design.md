# Terraform network and RAM for the Singapore storage experiment

Status: the user approved the proposed approach and then this written spec on October 3, 2026. Terraform configuration and offline verification are complete; see the [verification receipt](../../setup/sandbox-storage-network-verification.md). No resources have been provisioned by this slice.

## Purpose and accepted constraints

Prepare the networking and execution identity required to mount the dedicated AgenticFS Volume in disposable Singapore Agent Sandbox Gen2 instances. Success for this slice is validated Terraform configuration and a reviewable resource plan that feeds the merged storage bootstrap tool. Successful cloud mounting and Pi credential persistence are subsequent acceptance stages.

Direct user decisions: one public `jamesDeng/raptor-iap` repository; Aliyun Singapore; infrastructure managed through Terraform except the approved SDK storage exception; total Aliyun POC spending below RMB1,000; Pi calls OpenAI directly using the user's ChatGPT Pro session; one active Pi run per account. This slice does not implement that queue, handle model credentials, or deploy ACK, Argo CD, RDS, PgCat or the test application.

The user accepted the assistant's recommendation to create a dedicated network and execution role, stage scoped storage permissions after resource creation, and start without a NAT gateway. This does not establish that managed Internet egress and VPC access work together in the actual sandbox.

## Verified starting point

PR #1 is merged. Its AgenticFS setup/reconciliation/cleanup tool passes 22 offline tests and GitHub CI. Its cloud create/mount path remains unverified.

Read-only account queries on October 3 found zero Singapore VPCs, vSwitches and security groups. Existing RAM service-linked roles are not a dedicated storage execution identity and will not be edited. The China-account billing endpoint reported CNY1,000 available; that balance is not an accounting proof that the entire POC budget remains unspent. Delayed bills must be considered.

ECS reports Singapore zone A available, and Function Compute's supported-vSwitch documentation lists `ap-southeast-1a`. Actual Sandbox placement and connectivity still require a live test. Terraform is absent from the current PATH. The Singapore AgenticFS unit price has not been verified; live storage apply remains blocked.

## Ownership and repository layout

- `terraform-module/sandbox-storage-network/`: reusable VPC, vSwitch, ordinary security group, execution role and optional scoped NAS policy.
- `infra-terraform/environments/poc-sg-storage/`: one concrete root configuration for this experiment, provider/version constraints, input validation and outputs.
- `docs/setup/sandbox-storage-network.md`: local preparation, staged plans, outputs, acceptance checks and teardown order.
- A dedicated offline Terraform validation workflow: formatting, initialization without a backend, validation and tests using a mock provider. It receives no cloud credentials and does not apply changes.

Terraform owns network resources and RAM only. The existing SDK tool owns AgenticFS filesystem, AgenticSpace, Access Point and Volume metadata. Do not represent SDK objects as invented Terraform resources or lifecycle shell hooks. Do not modify unrelated image publishing or the bootstrap inventory schema.

## Network design

The root accepts only region `ap-southeast-1` and vSwitch zone `ap-southeast-1a` for this slice, matching the storage bootstrap contract. Proposed CIDRs are VPC `10.60.0.0/16` and vSwitch `10.60.1.0/24`. Validate containment, mask sizes and canonical network addresses. Before apply, check for overlap with any intended VPN, peering or future shared environment; the initial account query alone does not exclude external overlap.

Create one vSwitch. Multiple zones and high availability are unnecessary for the sequential persistence experiment. Use explicit names and project/environment tags so teardown and account inventory can identify Terraform-owned objects.

Create an ordinary, non-service-managed security group. Add no inbound allow rules. Explicit egress allows NFS TCP2049 to the dedicated VPC CIDR, TLS TCP443 to public destinations, and UDP/TCP53 for DNS. Domain-level restrictions are not promised by an IP security group; later sandbox network controls may narrow Internet destinations. Review provider-generated implicit rules, including same-group behavior, before claiming effective isolation. Do not add SSH, public IP addresses, NAT, ECS instances or a load balancer.

Use Agent Sandbox's documented VPC metadata to connect the sandbox to these resources. VPC attachment is not proof of successful NFS or model traffic. A live experiment must verify a mounted file read/write plus TLS/application responses from the actual OpenAI endpoints Pi uses. If managed public egress is unavailable with this configuration, stop and diagnose; price and review a NAT/EIP alternative separately before adding it.

## Execution role and staged policy

Create a dedicated execution role trusting `fc.aliyuncs.com` through `sts:AssumeRole`. Do not reuse or edit service-linked roles. Do not grant infrastructure administration, NAS create/delete control-plane actions, Volume APIs or model credentials to this role. Operator credentials and the sandbox control API key stay outside the sandbox.

There is a dependency cycle to resolve explicitly: Terraform's network is needed to create the Access Point; the resulting storage identifiers are needed to scope the execution policy. Use two normal Terraform plans rather than `-target` or wildcard interim permissions:

1. **Network stage:** filesystem ID and Access Point ID are both absent. Create VPC, vSwitch, security group and execution role, with no NAS data policy attached. This stage cannot mount storage.
2. **Storage stage:** after pricing and network review, the SDK tool creates its backing objects and records actual IDs. No model credentials are stored.
3. **Permission stage:** supply the recorded filesystem ID and Access Point ID together. Create and attach the NAS data policy through a second Terraform plan/apply. Reject configurations that provide only one ID, malformed IDs or another region/account's identifiers. Confirm IDs against the authenticated bootstrap inspection before this apply.
4. **Synthetic acceptance:** create a short-lived sandbox with the reviewed network and role; verify mount access, data persistence and OpenAI connectivity.

The policy permits `nas:ClientMount`, `nas:ClientWrite` and `nas:ClientRootAccess`, scoped to `acs:nas:ap-southeast-1:<account-id>:filesystem/<filesystem-id>` and conditioned on the exact `nas:AccessPointArn`. The initial action set follows Agent Sandbox's mount example. RootAccess is confined to the dedicated filesystem/Access Point; test whether the measured template user can operate without it before reducing the action set. General NAS documentation describes resource/Access Point scoping, but its AgenticFS behavior is not yet independently verified. A denial must not trigger automatic widening to `Resource: "*"`.

Terraform's provider authenticates using the existing supported credential chain outside the sandbox. Validate that its caller account matches the explicit account input before applying either stage. The execution role has no authority to provision its own network or storage. Any additional operator/service-linked permissions needed for VPC attachment must be diagnosed and documented separately, rather than copied into the execution role.

## Inputs, outputs and state

Inputs include account ID, fixed region/zone, project naming prefix, the two CIDRs and the paired optional storage IDs. No secret inputs are accepted. Account and storage identifiers are supplied through ignored local configuration, not illustrative deployed values committed to the public repository.

Outputs are exactly the network values the bootstrap config consumes: `vpc_id`, `vswitch_id`, `security_group_id`, `execution_role_arn`. Additional nonsecret policy/status outputs may indicate whether NAS permissions are attached, but must not change the existing bootstrap contract. Document copying the named entries from `terraform output -json` into its `terraform_outputs` object.

Use ignored local Terraform state for the first reviewed experiment. Protect its directory and files locally; state can contain account and infrastructure identifiers even though this module has no credential resources. Ignore state backups, plan files, output JSON and local variable files. Commit the provider lock file after resolving compatible exact versions. Remote state and GitHub Actions deployment/OIDC are separate future work; this workflow does offline validation only.

## Budget and live gates

Do not describe the network as free without checking applicable charges, including sandbox execution, network traffic and retention. The deployment deliberately omits NAT/EIP and continuously running compute, reducing the categories needing a quote.

Before SDK live apply, verify Singapore AgenticFS pricing from the official product pricing source, any minimum billable capacity, and the remaining overall POC budget. October monthly-to-hourly conversion uses 744 hours. A 24-hour storage estimate at the configured 10-GiB quota is `10 × hourly unit price × 24`; quota is not proof of a minimum billed allocation. Include sandbox runtime and all other POC charges when assigning the storage allowance. The current bootstrap evidence gate requires a positive verified rate; no dummy or zero rate may bypass it.

Apply requires a reviewed saved Terraform plan, matching account, reviewed network/role inputs and a current budget record. Pricing is an operator-reviewed quote, not a cloud-enforced spending cap. These preparations do not authorize an unreviewed live apply or storing OAuth credentials.

## Verification and acceptance

Offline acceptance checks must exercise meaningful boundaries: invalid region/zone, invalid or non-contained CIDRs, mismatched optional storage IDs, no NAS policy in the network stage, exact filesystem/Access Point scope in the permission stage, no added inbound rules or administrator policy, and outputs compatible with the bootstrap loader. Use Terraform mock-provider tests; they are not cloud connectivity evidence.

Pin a supported Terraform runtime/provider during implementation, verify formatting/validation and inspect provider lock changes. CI must not need account secrets. Generate and review a real account-aware plan only after the design and implementation plan are approved; a mock plan is not the live deployment plan.

Subsequent cloud acceptance: synthetic mounted marker survives confirmed termination of sandbox A and is readable in sandbox B; measured runtime permissions and atomic replacement/locking are tested; VPC storage and model HTTPS work in the same sandbox. Terminate temporary sandboxes. Do not introduce real OAuth secrets until these synthetic tests pass and the separate credential-persistence stage begins.

## Cleanup

Terminate and confirm all experiment sandboxes first. SDK cleanup must verify empty, owned storage and remove Volume metadata, Access Point, AgenticSpace and filesystem in dependency order. Do not silently erase retained credential data. Only after SDK-owned dependents are gone should a reviewed Terraform destroy plan remove policy/role and network resources. Preserve state and the nonsecret inventory while any dependent resource remains or a cloud outcome is uncertain.

## Sources and provenance

- Account query receipts, balance endpoint correction and proposal: [preparation notes](../../../../working/setup/2026-10-03-network-ram-preparation.md).
- User-approved persistence design and SDK exception: [persistence spec](../../../../working/design/2026-10-02-pi-credential-persistence.md).
- Merged tool and its verification scope: [bootstrap receipt](../../setup/agenticfs-bootstrap-verification.md).
- [Agent Sandbox VPC configuration](https://help.aliyun.com/zh/agent-sandbox/user-guide/vpc-network-configuration-1).
- [AgenticFS mount requirements](https://help.aliyun.com/zh/agent-sandbox/user-guide/mount-agenticfs-volume).
- [Sandbox Internet controls](https://help.aliyun.com/en/agent-sandbox/user-guide/network-access-control).
- [NAS scoped Access Point policies](https://help.aliyun.com/en/nas/user-guide/management-access-point).
- [FC supported vSwitch zones/network modes](https://help.aliyun.com/zh/functioncompute/configure-network-settings).
- [AgenticFS billing](https://help.aliyun.com/zh/nas/product-overview/billing-of-agenticfs) and [linked product pricing](https://www.aliyun.com/page-source/price/detail/markets/aliyun/nasnext).

Self-review: this spec covers Terraform network/RAM preparation only. Optional storage IDs represent explicit deployment stages, not unfilled requirements. No wildcard interim data grant, installed Terraform runtime, Singapore unit price, applied resources or successful mounting is asserted. The written spec and implementation plan were approved; the resulting configuration remains unapplied.
