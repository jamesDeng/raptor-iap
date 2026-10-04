# OSS-backed Pi sandbox lifecycle

Status: the reusable CLI passed normal → forced refresh → replacement normal acceptance in Aliyun Singapore on October 4, 2026. See [the sanitized receipt](oss-sandbox-lifecycle-acceptance.json). Independent metadata/inventory checks verified four archive/checksum pairs with AES256, the final selected checkpoint, zero active lifecycle sandboxes and zero remaining lifecycle control keys.

The local controller restores Pi OAuth credentials from an explicit checkpoint, runs a generated read-tool request in a Singapore Gen2 sandbox, saves a new immutable encrypted checkpoint, and confirms compute/control-key cleanup. Pi calls OpenAI directly. No Raptor UI, application queue, Infra API or infrastructure-changing tool is included.

## Preparation

Use Python 3.12, Node 22.23.3, Git and the official Aliyun CLI with the existing `infra-ops-poc` OAuth profile. From the repository root:

```sh
python3.12 -m venv .raptor-local/lifecycle-runtime
.raptor-local/lifecycle-runtime/bin/pip install -r tools/sandbox_lifecycle/requirements.txt
npm ci --ignore-scripts --no-audit --no-fund --prefix components/agent-harness
```

This dependency environment is separate from AgenticFS bootstrap. Credentials are captured privately from the CLI for the controller's lifetime. The controller bypasses process proxy environment variables for the direct Aliyun connection used in this POC; it does not change system proxy settings. Operator credentials/control keys never enter sandbox job files or checkpoints.

Put nonsecret account-specific config in a protected ignored `.raptor-local/lifecycle-config.json`. It must contain exactly: `account_id`, `region`, `team_id`, `template_id`, `bucket`, `bucket_prefix`, `volume_name`, `volume_id`, `execution_role_arn`, `bootstrap_checkpoint`, `model`, `sandbox_seconds`, `model_seconds`, `max_turns`, `max_output_tokens`.

Use region `ap-southeast-1`, model `gpt-5.6-luna`, limits 900/90/3/1024 in the same order as the final four fields, and the existing OSS mount prefix `auth`. Use the proven official Gen2 template for this slice. Do not substitute the published custom image without separately verifying that template integration.

The bootstrap checkpoint is an explicit object containing `archive_key`, `checksum_key`, `sha256`, `bytes`, `pi_version`. Import the retained probe's checkpoint 5 using its actual checksum and archive size from metadata-only reads; keep the values in ignored config. Controller metadata verification downloads only the nonsecret checksum. The credential archive is restored inside the sandbox, never on the Mac. Pi version is `0.99.2`. No object discovery by modification time or filename is used.

## Commands

Create a request file with exactly `{"kind":"read-probe","force_refresh":false}`. For the explicit refresh acceptance run, set `force_refresh` to true. Model limits come from config; requests cannot supply shell commands, prompts, credentials or arbitrary overrides.

```sh
.raptor-local/lifecycle-runtime/bin/python -m tools.sandbox_lifecycle status --config .raptor-local/lifecycle-config.json
.raptor-local/lifecycle-runtime/bin/python -m tools.sandbox_lifecycle run --config .raptor-local/lifecycle-config.json --request .raptor-local/read-request.json
.raptor-local/lifecycle-runtime/bin/python -m tools.sandbox_lifecycle recover --config .raptor-local/lifecycle-config.json
```

No command defaults to starting compute. The local ledger lives under the primary Git checkout's `.raptor-local/sandbox-lifecycle/<account-id>/ledger.json`, including when invoked from linked worktrees. Its account lock reports busy for a second local invocation. Use one checkout family/controller for this account; separate clones or hosts are not distributed-coordinated. A hosted queue is deferred.

Output distinguishes request completion, verified checkpoint publication and confirmed cleanup. Exit 0 means the selected lifecycle/status/recovery operation succeeded. Validation/busy/blocking uncertainty returns 2; attempted-operation failure returns 1. Recovery success is not a claim that an interrupted request completed.

## Failure and recovery

Recovery never reruns the prompt. It obtains a new temporary control key when needed, reads only a known completed runner-result file, terminates the old sandbox and confirms not-found before another run. If termination or key cleanup cannot be confirmed, the ledger retains IDs and blocks replacement.

An uncertain sandbox create without a returned ID remains blocked. Operator reconciliation must identify the `raptor.attempt` metadata and establish termination of any matching sandbox, or establish that none can remain (including the original 900-second expiry). There is no force/reset flag that bypasses this requirement. Preserve the ledger and ask the operator to resolve its recorded pending intent; do not delete it to unlock the account. A future inventory reconciliation interface may automate this case.

Malformed/missing/denied checkpoints stop before inference. Archives validate checksum, member paths/types, duplicate names and 16 MiB compressed/expanded-state limits before publishing restored local state. Live files use local permissions; OSS mount permissions are not claimed to protect credentials from the same runtime user.

Pi 0.99.2 uses `getDeviceId` during browser login. Its inspected refresh function uses the stored issued client ID, refresh token and resource, with no host-ID parameter. Each sandbox creates its own local host-ID file, excluded from archives; writing that file is not evidence of sending it during refresh. No OAuth protocol change is made here.

A normal request checkpoints after it settles. The explicit force-refresh acceptance step saves immediately before inference. Internal token refresh followed by crash before checkpoint remains an accepted loss window and may require sign-in again. Local ledger publication uses fsync/replace; OSS-mounted rename is not assumed atomic. Archives not selected by the ledger remain retained; automatic adoption/garbage collection is deferred.

## Verification and retention

Offline tests use synthetic credentials and external transport doubles. The recorded live acceptance ran normal → forced refresh → normal in confirmed sequential sandboxes. The middle report confirms both successful renewal and a changed refresh token; the replacement used it without browser authorization. Positive live tests use the retained credentials; destructive failure injection stays offline. Check billed costs as well as available balance when possible; the below-RMB1,000 budget is not enforced by this CLI.

Run/recover never delete the OSS bucket, Volume, role, retained credential data or Terraform state. These retain storage/request charges. Deleting retained credentials or Terraform resources requires a separate reviewed teardown. New sandbox/control-key operations are bounded and temporary.

The October account bill query returned three product summaries totaling CNY 0.0 pretax at verification time; delayed charges may still arrive. Available balance was RMB 1,000 at preflight. Neither observation establishes a hard budget cap or final total spend. Diagnostic failures and fixes preceding the final clean CLI sequence are retained in the receipt.

The [review record](oss-sandbox-lifecycle-review.md) lists the six findings, regression fixes and final local checks. Persisted results use the same phase-specific contracts as fresh runner results. Cleanup proceeds despite result-read or ledger-write failures; `persistence_error` reports ledger uncertainty separately from confirmed cloud cleanup. The retained Volume omits `mount_config`, so sandbox create supplies the configured role explicitly; conflicting declared roles are rejected.
