# Singapore sandbox storage networking

Terraform owns a dedicated VPC, zone-A vSwitch, ordinary security group and Function Compute execution role. An optional NAS policy is scoped to the filesystem and Access Point recorded by the SDK bootstrap tool. This does not deploy ACK/RDS/PgCat or persist Pi credentials.

## Runtime and offline verification

Use Terraform **1.13.3** and provider **aliyun/alicloud 1.293.0**. Verify the runtime archive against HashiCorp's [checksum manifest](https://releases.hashicorp.com/terraform/1.13.3/terraform_1.13.3_SHA256SUMS). The implementation's ignored local binary is `.raptor-local/bin/terraform`; add that directory to PATH for these commands. Lock files are committed for darwin_arm64 and linux_amd64.

```sh
terraform fmt -check -recursive infra-terraform terraform-module tests/fixtures
terraform -chdir=terraform-module/sandbox-storage-network init -backend=false -lockfile=readonly
terraform -chdir=terraform-module/sandbox-storage-network validate
terraform -chdir=terraform-module/sandbox-storage-network test
terraform -chdir=infra-terraform/environments/poc-sg-storage init -backend=false -lockfile=readonly
terraform -chdir=infra-terraform/environments/poc-sg-storage validate
terraform -chdir=infra-terraform/environments/poc-sg-storage test
TERRAFORM_BIN=terraform .venv/bin/python -m unittest discover -s tests -v
```

All Terraform tests use a mock provider. The Python contract test applies only a local `terraform_data` fixture in a temporary directory; it has no external provider and creates no cloud resource. Mock outputs and local serialization are separate from cloud acceptance. Restricted local process execution may prevent provider socket startup even in mocked tests; this is a runner limitation, not evidence that the cloud configuration fails.

## Credentials and private artifacts

The pinned provider supports the existing Aliyun CLI profile through `ALIBABA_CLOUD_PROFILE=infra-ops-poc`, with its default file `~/.aliyun/config.json`. An alternate path uses `ALIBABA_CLOUD_CREDENTIALS_FILE`. See the [pinned provider configuration](https://github.com/aliyun/terraform-provider-alicloud/blob/v1.293.0/website/docs/index.html.markdown). This behavior is specific to Terraform; the Python storage tool's SDK chain does not automatically import a CLI profile.

Do not put access keys in HCL, command arguments, state inputs or Git. The module reads the caller account during planning, then schedules another authenticated read at apply using a local timestamp dependency. Both checks must match before dependent cloud resources can be created or updated. This prevents a saved plan from reusing only an earlier account identity; the second check remains unknown in the saved plan until execution. The local timestamp record changes on every normal apply. Keep operator credentials and the Sandbox API key outside sandbox instances; the execution role receives only NAS data access after stage two.

The root local backend stores state under repository `.raptor-local/poc-sg-storage/`, separate from the tracked source directory. Use `umask 077`, a local directory with mode 0700 and files with mode 0600. State, plans, backups, input variables and output JSON are ignored; they can still contain private infrastructure metadata. Preserve these files while resources or uncertain outcomes remain. CI gets no cloud credentials and never runs a real plan/apply.

## Stage one: network plan

Set `TF_VAR_account_id` to the authenticated account ID and `ALIBABA_CLOUD_PROFILE` to the intended profile. Keep both storage ID inputs null. Default CIDRs are `10.60.0.0/16` and `10.60.1.0/24`; the module accepts canonical IPv4 /16 and /24 only. Check VPN/peering overlap before apply. Region is Singapore, zone A.

```sh
terraform -chdir=infra-terraform/environments/poc-sg-storage init -lockfile=readonly
terraform -chdir=infra-terraform/environments/poc-sg-storage plan -input=false -out=../../../.raptor-local/network.tfplan
terraform -chdir=infra-terraform/environments/poc-sg-storage show ../../../.raptor-local/network.tfplan
```

Create the ignored plan directory before running these commands; relative paths above resolve to repository `.raptor-local/`. Inspect the saved plan before separately authorizing apply. The role has no NAS policy at this stage and cannot mount storage. No NAT/EIP or persistent compute is included. Network and role creation alone do not prove NFS, OpenAI egress or effective security-group isolation.

The security group explicitly denies IPv4 ingress/egress at priority 100 and allows TCP2049 inside the VPC, TCP443 and UDP/TCP53 at priority 1. `inner_access_policy=Drop` requests same-group isolation. Inspect actual defaults and live behavior later. TLS/DNS egress allows public destinations by IP; it is not a model-domain allowlist. No IPv6 network is configured.

## Storage and stage two: scoped permissions

After reviewed network apply, export `terraform output -json` to an ignored local file. Copy the four named output entries `vpc_id`, `vswitch_id`, `security_group_id`, `execution_role_arn` into the bootstrap config's `terraform_outputs` object. Its account, Team and measured UID/GID must be real inputs, not test fixtures.

Verify Singapore AgenticFS unit price, minimum charges and the remaining overall POC budget before the SDK apply. The latest balance alone does not include all delayed costs. Record positive pricing evidence in the bootstrap gate; do not substitute a dummy rate. Run the existing [gated bootstrap](agenticfs-bootstrap.md), then inspect ownership/account/region and relationships with its read-only inspection command.

Supply both resulting `filesystem_id` and `access_point_id` in an ignored local variable file. An ID is not proof of ownership; authenticated inspection is required before the permission plan. Terraform rejects partial pairs and malformed identifiers. Generate/review a second full plan; do not use `-target` or wildcard interim NAS permissions. It adds one custom policy and one attachment, granting only ClientMount, ClientWrite and ClientRootAccess on the exact filesystem with an exact Access Point ARN condition.

General NAS documentation supports that scope, but AgenticFS mount compatibility must be tested. On an access denial, stop and diagnose. Do not automatically widen to `Resource: "*"` or attach administrator permissions, and do not modify service-linked roles. Any extra operator or service-linked network permissions are a separate diagnosis. Test whether the actual runtime user needs RootAccess before reducing that action.

## Later synthetic acceptance

Launch short-lived sandbox A with the VPC metadata, execution role and Volume mount. Test synthetic data permissions, file locking/atomic replacement and OpenAI HTTPS in the same instance. Confirm A is terminated before launching B; verify B can read the marker. Terminate both. These tests are not part of offline acceptance and do not use real OAuth credentials.

Start without NAT. If managed Internet access fails with VPC attachment, diagnose and price a separately reviewed NAT/EIP alternative. A working mount alone does not verify model access or renewable credential persistence.

## Teardown

Confirm dependent sandboxes are terminated. Preserve retained data unless its deletion is explicitly authorized. Use the SDK's verified-empty cleanup to remove Volume metadata, Access Point, AgenticSpace and filesystem. Removing Volume metadata alone does not stop backing-storage billing.

Only after SDK dependents are gone, independently verify the current authenticated account, inspect a Terraform destroy plan for this dedicated root, then obtain authorization to execute it. Lifecycle preconditions are not a destroy-time account boundary; a separately verified execution preflight is required before any future destroy. Keep state and the SDK inventory for reconciliation if any outcome is uncertain. No automatic apply/destroy command is supplied by this slice.
