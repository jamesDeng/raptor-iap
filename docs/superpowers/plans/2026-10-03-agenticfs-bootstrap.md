# AgenticFS storage bootstrap implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for inline execution or superpowers:subagent-driven-development if the user selects delegation. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Build and locally verify a repeatable setup/cleanup tool for storage objects missing from the official Terraform provider.

**Architecture:** Terraform owns networking and RAM; the approved SDK exception owns the AgenticFS filesystem, space, Access Point and Team-scoped Sandbox Volume. The tool consumes nonsecret Terraform outputs, keeps an atomic resource inventory outside Git, and reconciles resources through official APIs. This slice does not provision cloud resources or persist ChatGPT credentials.

**Tech Stack:** Python 3, official Alibaba Cloud credential/Tea OpenAPI/NAS SDKs and fcsandbox SDK (>=1.3.0), unittest with injected fake clients. Pin resolved compatible SDK versions after verifying their schemas; no cloud calls during tests.

**Spec:** /Users/dengwei/Documents/Codex/2026-09-26/f/outputs/infra-ops-agent/working/design/2026-10-02-pi-credential-persistence.md (approved October 3, including SDK exception).

## Global constraints

- Region `ap-southeast-1`; AgenticSpace zone `ap-southeast-1a`.
- One public repository; secrets and local resource inventories remain outside Git and images.
- Total Aliyun spend below RMB 1,000; an unverified price or missing remaining-budget evidence blocks provisioning.
- Pi calls OpenAI directly; model `gpt-5.6-luna`; one active Pi run per Pro account. This slice does not implement Pi sessions or the platform queue.
- Terraform continues to own supported networking/RAM. No ad hoc networking changes by this tool.
- No support ticket submission or automatic fallback to another storage product.
- Retain credential storage for later authorized runs. Cleanup must not erase credential data without a separate explicit user request.
- Existing cloud resources are not adopted solely because their names match.

## Review focus

- Interrupted creates: journal intent before mutation; reconcile uncertain outcomes before retrying to avoid duplicate billable resources.
- Wrong account/region/Team: reject mismatch before any write.
- Partial cleanup: preserve remaining inventory and report the failure; never delete parents after failed child deletion.
- Sensitive SDK errors: expose only allowlisted error codes/request IDs, never raw response bodies or credential data.
- Existing stored data or active consumers: stop cleanup; do not silently force deletion.

## File map and scope

- `tools/agenticfs_bootstrap/config.py`: validated inputs and nonsecret inventory.
- `tools/agenticfs_bootstrap/api.py`: official SDK adapter with injected clients.
- `tools/agenticfs_bootstrap/lifecycle.py`: setup/reconciliation/cleanup.
- `tools/agenticfs_bootstrap/__main__.py`: inspect, plan, apply and cleanup-plan entry points.
- `tools/agenticfs_bootstrap/requirements.txt`: compatible exact SDK pins.
- `tests/test_agenticfs_bootstrap.py`: offline tests.
- `docs/setup/agenticfs-bootstrap.md`: ownership, usage, known limits and later cloud gates.
- `.gitignore`: exclude resource inventory, Terraform output files and credentials.

## Task 1: Input contract and durable nonsecret inventory

**Interfaces:** `load_config(path: Path) -> BootstrapConfig`; `read_inventory(path: Path) -> Inventory`; `save_inventory(path: Path, inventory: Inventory) -> None`.

Configuration requires account_id, region, zone, team_id, vpc_id, vswitch_id, security_group_id, execution_role_arn, runtime_uid, runtime_gid and run_id. Terraform output JSON supplies the network/role values. Runtime UID/GID must be measured from the selected template, not assumed from documentation examples. Inventory stores schema_version=1, config fingerprint, run_id, phase, client tokens, filesystem_id, agentic_space_id, access_point_id, access_point_domain and volume_id/name. No auth record, API key or secret belongs in either file.

- [x] Write `test_config_rejects_wrong_region_and_missing_outputs`, `test_inventory_rejects_unknown_secret_fields`, `test_inventory_replace_preserves_previous_record_on_failure`: assert invalid inputs raise before any client call; schema forbids access_token, refresh_token and api_key; failed atomic replace leaves the previous complete record readable.
- [x] Run `python3 -m unittest discover -s tests -p 'test_agenticfs_bootstrap.py' -v`; verify the new tests fail because the implementation is absent.
- [x] Implement the three interfaces with strict field validation, same-directory temporary file, fsync, replace and directory fsync; inventory permissions 0600, parent 0700.
- [x] Rerun the tests and verify all assertions pass.
- [x] Commit this task with its tests and inventory ignore rules.

## Task 2: SDK adapters and recoverable setup

**Interfaces:** `StorageAPI.create_filesystem(client_token: str) -> str`; `get_filesystem(id: str) -> dict`; `create_space(filesystem_id: str, client_token: str) -> str`; `get_space(filesystem_id: str, space_id: str) -> dict`; `create_access_point(filesystem_id: str, space_id: str) -> tuple[str, str]`; `get_access_point(id: str) -> dict`; `create_volume(name: str, server_addr: str, uid: int, gid: int) -> str`; `get_volume(id: str) -> dict`; `reconcile_pending(config: BootstrapConfig, inventory: Inventory) -> Inventory`; `ensure_storage(api: StorageAPI, config: BootstrapConfig, inventory_path: Path) -> Inventory`.

Resolve the official SDK schemas before pinning dependencies. Use the normal credential provider chain; never pass credential values into CLI arguments or inventory. Fail with a named unsupported-schema error if a selected NAS SDK cannot express AgenticSpaceId or the space APIs. Credentials/configuration must be bound to the checked account identity.

API sequence: CreateFileSystem(StorageType=Agentic, ProtocolType=NFS, FileSystemType=standard), wait Running; CreateAgenticSpace(Azone=ap-southeast-1a, FileSystemPath=/raptor-<run_id>, Quota.SizeLimit=10737418240, Quota.FileCountLimit=10000); CreateAccessPoint(FileSystemId, AgenticSpaceId, VpcId, VswId, EnabledRam=true), wait active; fcsandbox CreateVolume using actual AccessPointDomain with `:/` and measured UID/GID. The 10-GiB value is an API quota ceiling, not a claim about minimum billed usage.

- [x] Write `test_setup_request_fields_and_order`, `test_rerun_reuses_recorded_ids`, `test_uncertain_create_stops_before_duplicate`, `test_wrong_team_volume_is_rejected`, `test_sdk_error_is_redacted`: assert the exact API fields above; AccessGroup, RootDirectory and PosixUserId absent; second run makes zero creates; uncertain outcome requires uniquely matching inventory intent; mismatch produces zero writes; secret-bearing fake error text never appears in output.
- [x] Run the offline test command and verify new tests fail.
- [x] Implement adapters and lifecycle. Record intent/client tokens before calls, successful IDs immediately afterwards, bounded readiness waits of 120 seconds with five-second polls. For APIs without idempotency tokens, reconcile by actual list/get data and run-specific intent; if identity is ambiguous, stop. Serialize invocations with a local exclusive lock; this tool is not distributed orchestration.
- [x] Run all offline tests; resolve/pin SDK versions and verify construction/serialization against installed SDK models without calling the cloud.
- [x] Commit adapters, lifecycle and tests.

## Task 3: Inspection, plans and safe cleanup

**Interfaces:** `build_plan(config: BootstrapConfig, inventory: Inventory) -> dict`; `cleanup_plan(api: StorageAPI, inventory: Inventory, active_sandbox_ids: list[str]) -> dict`; `cleanup_empty_storage(api: StorageAPI, inventory_path: Path, active_sandbox_ids: list[str], allow_empty_delete: bool) -> Inventory`; `main(argv: list[str]) -> int`.

Extend `StorageAPI` with `delete_volume(id: str) -> None`, `delete_access_point(id: str) -> None`, `delete_space(filesystem_id: str, space_id: str) -> None`, `delete_filesystem(id: str) -> None`, and `space_is_empty(filesystem_id: str, space_id: str) -> bool`. These wrap the corresponding official delete/get APIs; the emptiness method must raise an unverifiable-occupancy error rather than treating absent usage fields as zero. The command checks live sandbox attachment information; a caller's empty list alone is not evidence that no sandbox uses the volume.

Plan output lists SDK-owned objects, dependencies and retention. Apply is explicit, requires recorded account/region/Team match, verified pricing/remaining budget and reviewed network/role inputs; absent evidence returns a blocking error. This plan authorizes building the tool, not live apply. SDK deletion order is Volume metadata → Access Point → AgenticSpace → filesystem. Only tool-owned resources are eligible; backend occupancy must be verified as empty, all dependent sandboxes terminated, and explicit empty-delete selection provided. If occupancy cannot be established, cleanup stops. Never remove files/auth records as part of infrastructure cleanup. Terraform networking teardown occurs separately after SDK-owned dependents are gone.

- [x] Write `test_default_command_does_not_mutate`, `test_apply_without_price_evidence_stops`, `test_cleanup_refuses_active_consumer_or_nonempty_space`, `test_child_delete_failure_preserves_parent`, `test_cleanup_rejects_foreign_inventory`: assert zero mutations in each blocked case; failure leaves parents untouched and remaining IDs saved.
- [x] Run the offline suite and verify these tests fail.
- [x] Implement the interfaces and docs. Allowlisted output includes stage, resource IDs, request ID, error code and remaining-resource count. Cleanup plan is the default; executing cleanup is an explicit separate command.
- [x] Verify the full suite passes, invoke the module with `--help` and a synthetic plan fixture, scan tracked changes for secret fixtures, and review the final diff. No credentials or paid resources required for acceptance.
- [x] Commit CLI, cleanup and documentation.

## Later cloud acceptance gate

After this slice, collect the Singapore price and remaining-budget evidence, finalize/review Terraform networking/RAM configuration, and inspect its plan before live apply. Then validate SDK create/get readiness and synthetic volume write → terminate A → read in B. Only after that passes, implement the Pi atomic credential backend and A/B/C refresh persistence experiment from the approved spec. A successful dry-run or offline suite does not satisfy those cloud stages.

## Sources and self-review

- Official CreateAccessPoint: https://help.aliyun.com/zh/nas/developer-reference/api-nas-2017-06-26-createaccesspoint
- Official CreateAgenticSpace: https://help.aliyun.com/zh/nas/developer-reference/api-nas-2017-06-26-createagenticspace
- Official Volume lifecycle: https://help.aliyun.com/en/agent-sandbox/user-guide/create-an-agenticfs-volume
- Billing: https://help.aliyun.com/zh/nas/product-overview/billing-of-agenticfs
- Provider investigation: ../../../../working/setup/2026-10-03-agenticfs-terraform-coverage.md

Self-review: this plan covers the approved infrastructure exception only. Each review-focus failure has a named offline test. The full spec's model renewal, single-active platform queue and cloud persistence acceptance remain explicitly outside this implementation slice. No invented Terraform resource or deployed-function claim is used.
