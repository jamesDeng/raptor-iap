# Dedicated AgenticFS storage bootstrap

This is the approved POC exception for APIs missing from the official Terraform provider. It manages one dedicated AgenticFS filesystem, one AgenticSpace, its RAM-enabled Access Point and one Sandbox Volume. Networking and execution-role permissions remain Terraform-owned. This tool does not sign in to OpenAI, handle Pi credentials, deploy an application or mount a sandbox.

## Current evidence

Request construction uses the installed official NAS 3.7.3, fcsandbox 1.6.0 and STS 1.2.0 SDK models. Lifecycle tests replace only external SDK calls; no cloud resource was created by these tests. Actual creation, mounting and credential persistence remain unverified. The earlier Singapore CreateFileSystem dry-run passed; that is not evidence the whole chain will work.

## Local setup

Run from the repository root with Python 3.12+ and OpenSSL support. The macOS system Python 3.9/LibreSSL runtime produces an urllib3 compatibility warning; use a modern Python instead.

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r tools/agenticfs_bootstrap/requirements.txt
.venv/bin/python -m unittest discover -s tests -v
.venv/bin/python -m tools.agenticfs_bootstrap --help
```

Dependencies are pinned, including transitives, from the temporary clean verification environment. They contain no private packages or credentials. Local tests need no Aliyun or OpenAI credentials.

## Configuration

Save inputs in `.raptor-local/agenticfs/config.json`; this directory is ignored. Supply actual IDs from reviewed Terraform outputs, an actual Team ID, and UID/GID measured inside the selected sandbox template. The values below are illustrative and not deployed resources:

```json
{
  "account_id": "1234567890123456",
  "region": "ap-southeast-1",
  "zone": "ap-southeast-1a",
  "team_id": "replace-with-actual-team",
  "run_id": "poc-storage",
  "runtime_uid": 1000,
  "runtime_gid": 1000,
  "terraform_outputs": {
    "vpc_id": {"value": "vpc-replace"},
    "vswitch_id": {"value": "vsw-replace"},
    "security_group_id": {"value": "sg-replace"},
    "execution_role_arn": {"value": "acs:ram::1234567890123456:role/replace"}
  }
}
```

Copy the named fields from `terraform output -json`; additional Terraform output fields are not consumed. Do not put secrets in this file. The tool does not create these network/role resources and does not independently verify that their configuration provides network connectivity or mount permission.

## Default: local plan only

```sh
.venv/bin/python -m tools.agenticfs_bootstrap \
  --config .raptor-local/agenticfs/config.json
```

This does not construct cloud clients, resolve credentials, create inventory or call an API. It reports the desired objects and the configuration fingerprint. The 10-GiB space quota is an API limit, not a minimum billed capacity. Billing is based on actual hourly peak stored capacity.

## Live setup — gated, not performed yet

Live commands use the official Alibaba Cloud credential provider chain. An Aliyun CLI profile is not automatically imported by this tool. Supply credentials using a supported SDK credential source (environment, provider configuration or workload identity), without putting keys in shell arguments, Git or inventory. STS GetCallerIdentity must match the configured account before mutations.

Before apply, save a reviewed evidence record outside Git with these fields:

- `checked_at`: timezone-aware ISO timestamp, no older than 24 hours.
- `configuration_fingerprint`: the fingerprint printed by the local plan.
- `network_reviewed`: true only after Terraform networking and role configuration has been reviewed.
- `price_source`: the official Aliyun pricing page used for the quote.
- `hourly_price_rmb_per_gib`: verified Singapore unit price in RMB/GiB/hour, greater than zero.
- `remaining_budget_rmb`: actual remaining POC budget, greater than zero and at most 1,000.
- `storage_budget_rmb`: amount allocated to this storage experiment, no larger than the remaining budget.
- `max_hours`: planned experiment duration, at most 24 hours.

Apply checks `10 × hourly price × planned hours <= storage budget`. This is a conservative storage estimate at the space quota, not a cloud-enforced spending cap. The evidence is an operator-reviewed record, not independent verification of pricing. Continued storage retention, other cloud services and delayed billing can still consume budget; review charges and teardown separately.

```sh
.venv/bin/python -m tools.agenticfs_bootstrap apply \
  --config .raptor-local/agenticfs/config.json \
  --gate .raptor-local/agenticfs/reviewed-evidence.json
```

Inspect is read-only:

```sh
.venv/bin/python -m tools.agenticfs_bootstrap inspect \
  --config .raptor-local/agenticfs/config.json
```

Resource inventory is atomically saved with 0600 permissions in a 0700 directory. It contains IDs, random ownership tags and client tokens, never model credentials. A local lock serializes this tool's invocations on one host; it is not a distributed lease or the platform's single-active-Pi-run queue.

Interrupted creates retain a pending marker. A rerun searches for the journaled random ownership tag/nonce and matching resource relationships. If no unique match can be proved, it stops with `UncertainCreate`; do not delete the journal and blindly retry. Keep the inventory for manual reconciliation. Existing resources are not adopted based on a familiar name.

## Cleanup without erasing data

Start with `cleanup-plan`, which checks ownership, children, occupancy and consumers without deleting anything. It requires a Team-bound sandbox key supplied through `E2B_API_KEY` and its nonsecret identifier through `E2B_API_KEY_ID`. The POP DescribeApiKey response must confirm the Team and full key value; otherwise the tool stops. Key values stay in memory and are not printed. The key is used only to query sandbox consumers, not model access.

Any running or paused sandbox in that Team blocks cleanup conservatively. Missing permissions, masked key values, missing usage metrics or an inconclusive consumer query also block cleanup.

```sh
.venv/bin/python -m tools.agenticfs_bootstrap cleanup-plan \
  --config .raptor-local/agenticfs/config.json
```

Explicit cleanup of empty, unused, tool-owned storage:

```sh
.venv/bin/python -m tools.agenticfs_bootstrap cleanup --allow-empty-delete \
  --config .raptor-local/agenticfs/config.json
```

Deletion order is Volume metadata → Access Point → AgenticSpace → filesystem. Each deletion must be confirmed before the parent is touched. Failure keeps the remaining IDs. A rerun checks the pending deletion and resource ownership before resuming. No file removal, force-delete or credential erasure is implemented. Do not run cleanup for storage retaining the Pro session; retaining it is the normal POC path. Removing Volume metadata alone does not stop backing-storage billing.

There is no distributed exclusion of other operators or controllers. Stop external sandbox creation while reviewing/executing cleanup; the checks cannot prevent another actor from starting a sandbox after a query. Default SDK errors are redacted to static codes to avoid accidental secret output; use the console for additional diagnostics without pasting credentials into chat.

## Next verification stages

1. Verify price and remaining budget, then review/create Terraform-owned network and execution role.
2. Run SDK setup and inspect actual readiness; handle any account-preview errors as observed facts.
3. Mount synthetic data in sandbox A, terminate A, and read it in B. Verify UID/GID, permissions, locking and atomic replacement.
4. Only then add the Pi atomic credential backend and the authorized A/B/C token-refresh persistence experiment.

## Official references

- [AgenticSpace creation](https://help.aliyun.com/zh/nas/developer-reference/api-nas-2017-06-26-createagenticspace)
- [Access Point creation](https://help.aliyun.com/zh/nas/developer-reference/api-nas-2017-06-26-createaccesspoint)
- [Sandbox Volume creation](https://help.aliyun.com/en/agent-sandbox/user-guide/create-an-agenticfs-volume)
- [Volume mounting](https://help.aliyun.com/zh/agent-sandbox/user-guide/mount-agenticfs-volume)
- [AgenticFS billing](https://help.aliyun.com/zh/nas/product-overview/billing-of-agenticfs)
