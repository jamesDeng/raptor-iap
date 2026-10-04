# OSS-backed Pi Sandbox Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended by the skill) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. For this tightly coupled first slice, the project recommendation is native execution; the user chooses the execution method after reviewing this plan.

**Goal:** Run bounded Pi requests in sequential Singapore sandboxes with restored OAuth credentials, verified encrypted checkpoints and explicit interrupted-run recovery.

**Architecture:** A local Python controller owns a protected run ledger and single-account lock, calls the sandbox SDK and validates checkpoint object metadata. A Node runner keeps Pi state local, performs a bounded read-tool request and writes immutable OSS snapshots. The ledger selects the last verified checkpoint; uncertain outcomes block new runs.

**Tech Stack:** Python 3.12 and unittest; Node 22.23.3 and node:test; Pi 0.99.2; E2B 2.31.0; Aliyun FCSandbox SDK 1.6.0; OSS SDK 2.19.1; existing Singapore OSS Volume and scoped execution role.

**Spec:** [Approved design](../specs/2026-10-04-oss-sandbox-lifecycle.md).

Status: approved implementation plan, October 4, 2026; native execution authorized. Task checklists below preserve the original plan, rather than serving as a live progress ledger. See the operator guide for actual verification. All repository paths below are relative to `raptor-iap/`. Execution begins in an isolated worktree; preserve the original checkout's ignored credential storage and Terraform state.

## Global Constraints

- Region `ap-southeast-1`; Pi `0.99.2`; Node `22.23.3`; runtime E2B `2.31.0`. Keep AgenticFS bootstrap dependencies unchanged.
- GPT-5.6 Luna (`gpt-5.6-luna`) using the existing ChatGPT Pro OAuth session; Pi calls OpenAI directly.
- One controller/active run per account on this Mac; no distributed coordination claim or new hosted queue.
- Sandbox lifetime 900 seconds; model timeout 90 seconds; maximum three assistant turns; maximum 1,024 output tokens per response; no automatic whole-request retries.
- Checkpoint compressed archive and expanded contents each at most 16 MiB; local state directories 0700 and credential files 0600.
- Archive contents only `auth.json` and `sessions/`; no host IDs, fixtures, locks, operator secrets or source code.
- Immutable new object names under `auth/lifecycle/`; preserve retained probe checkpoints 1–5. Existing Volume mounts `auth/` at `/mnt/oss`.
- Latest-checkpoint publication only after readback/hash and controller metadata verification. No FUSE rename atomicity assumption.
- Checkpoint after requests and orderly failure; refresh-before-checkpoint crash loss remains accepted and unresolved.
- Sandbox ID and temporary control-key ID persist until confirmed cleanup. Keys expire within one hour. No automatic retained-storage deletion.
- Public repository and offline CI contain no real account config, OAuth data, API keys, callbacks or raw provider errors.
- Total Aliyun POC budget below RMB1,000; reuse existing persistent resources. This slice does not enforce an account-wide billing cap.

## Review Focus

1. A config or ledger copied to another account/bucket must not restore foreign state; pin configuration identity and storage prefix in Task 1/4 tests.
2. An SDK timeout after successful remote creation must not trigger another sandbox/key creation; persist pending intent and block until reconciled in Tasks 4/5.
3. A complete model answer followed by failed checkpointing must remain a failed lifecycle result, preserving prior checkpoint and cleanup obligations in Task 5.
4. A corrupt archive must never write outside the restore root or expand without limit, including symlinks, duplicate entries and gzip bombs in Task 2.
5. Provider failures and request/tool text must not enter public reports; exercise synthetic secret markers and exact allowlists in Tasks 3/6.

## File map and shared contracts

- `tools/sandbox_lifecycle/config.py`: strict config/request/checkpoint dataclasses and validation.
- `tools/sandbox_lifecycle/state.py`: canonical account state directory, lock, atomic ledger writes.
- `tools/sandbox_lifecycle/cloud.py`: SDK/operator identity, temporary keys, sandbox actions and metadata checks.
- `tools/sandbox_lifecycle/lifecycle.py`: orchestration, error transitions, cleanup and recovery.
- `tools/sandbox_lifecycle/__main__.py`: status/run/recover CLI and safe reporting.
- `tools/sandbox_lifecycle/requirements.txt`: separate fully pinned Python runtime.
- `components/agent-harness/archive.py`: standard-library safe tar/gzip pack/restore, uploaded beside the runner.
- `components/agent-harness/checkpoint.mjs`: immutable mounted snapshots and allowlisted descriptors.
- `components/agent-harness/pi-adapter.mjs`: pinned Pi import, credential loading/refresh and bounded inference.
- `components/agent-harness/runner.mjs`: protected job input/output, restoration, dispatch and final checkpoint.
- `components/agent-harness/package.json`, `package-lock.json`: exact Pi dependency and test scripts.
- `tests/test_sandbox_lifecycle_{state,archive,cloud,lifecycle,cli}.py`, `tests/harness/*.test.mjs`: meaningful boundary tests; shared synthetic fixture helpers under `tests/fixtures/sandbox_lifecycle/`.
- `.github/workflows/sandbox-lifecycle.yml`, `docs/setup/oss-sandbox-lifecycle.md`, `README.md`: offline CI and operator documentation.

Define dataclasses in `config.py`: `CheckpointRef(archive_key: str, checksum_key: str, sha256: str, bytes: int, pi_version: str)`; `Request(kind: str, force_refresh: bool)`; `Config(account_id, region, team_id, template_id, bucket, bucket_prefix, volume_name, volume_id, execution_role_arn, bootstrap_checkpoint, model, sandbox_seconds, model_seconds, max_turns, max_output_tokens)` with field types from this plan. `bootstrap_checkpoint` is a `CheckpointRef`; all limits are integers. `Request.kind` initially accepts only `read-probe`; this is the lifecycle acceptance boundary, not a new infrastructure workflow.

Ledger schema v1: configuration fingerprint, attempt UUID, phase enum, sandbox/key IDs when known, pending-create/key intent, selected checkpoint, candidate checkpoint, and a safe outcome enum. No arbitrary extension fields. Distinguish request outcome (`completed`, `failed`, `unknown`) from checkpoint and cleanup outcomes. SDK secrets never serialize in the ledger.

### Task 1: Durable local ownership and inputs

**Files:** Create `config.py`, `state.py`, `tests/test_sandbox_lifecycle_state.py` and shared fixtures.

**Interfaces:** `load_config(path: Path) -> Config`; `load_request(path: Path) -> Request`; `validate_checkpoint(value: dict, cfg: Config, bootstrap: bool=False) -> CheckpointRef`; `account_lock(state_root: Path, account_id: str) -> ContextManager`; `load_ledger(path: Path, cfg: Config) -> dict`; `save_ledger(path: Path, value: dict) -> None`.

- [ ] Write failing tests: `test_wrong_region_role_account_or_unknown_secret_field_rejected` asserts fixed region, matching role ARN and strict field allowlist; `test_boolean_or_excessive_limits_rejected` rejects bool as int and limits above global maxima; `test_second_process_cannot_acquire_same_account_lock` uses real processes; `test_fingerprint_blocks_foreign_ledger` refuses another bucket/config; `test_failed_replace_preserves_previous_checkpoint` injects replace failure and reads the prior real file; `test_symlink_state_path_rejected` protects the canonical ignored state path; `test_state_permissions` asserts 0700/0600.
- [ ] Run `python3 -m unittest discover -s tests -p test_sandbox_lifecycle_state.py -v`; confirm the expected missing implementation or failing behavior.
- [ ] Implement strict dataclasses and canonical `.raptor-local/sandbox-lifecycle/<account-id>/` state directory. Lock with nonblocking POSIX flock; use exclusive file creation/O_NOFOLLOW checks. Ledger writes use temp file, fsync, replace and directory fsync. A read failure never becomes an empty ledger.
- [ ] Repeat the command; require all state tests pass. Commit this independently testable boundary.

### Task 2: Safe portable checkpoints

**Files:** Create `archive.py`, `checkpoint.mjs`, `tests/test_sandbox_lifecycle_archive.py`, `tests/harness/checkpoint.test.mjs`.

**Interfaces:** Python `pack_state(root: Path, archive: Path, limit: int=16777216) -> dict` and `restore_state(archive: Path, root: Path, expected_sha256: str, limit: int=16777216) -> None`. Node `createCheckpoint({stateRoot, mountRoot, generation}) -> Promise<descriptor>` and `restoreCheckpoint({reference, stateRoot, mountRoot}) -> Promise<void>`. Descriptor matches `CheckpointRef` in snake_case. `generation` is a UUID, not a sequence number.

- [ ] Write failing tests: round-trip synthetic full OAuth metadata and session bytes; exclude host ID; reject absolute/traversal/link/device/duplicate paths and unexpected files; reject both archive and expanded size above 16 MiB; bounded gzip decompression; reject checksum mismatch and malformed auth without secret-valued exception text. Assert failed restore leaves the target unpublished. Test immutable-name collision preserves old bytes, and failed readback produces no descriptor.
- [ ] Run the Python archive suite and `node --test tests/harness/checkpoint.test.mjs`; observe expected failures.
- [ ] Implement explicit allowlisted member validation and bounded extraction into a fresh local staging directory, then local publication only after full validation. Require a top-level `auth.json` with a Pi OAuth record and preserve its opaque fields; protect output modes. Use Python's standard tar/gzip libraries, not unsafe shell extraction. Reject symlinks in source/mount destination paths. Create unique archives/checksums exclusively, verify both by readback; return metadata only. Never overwrite original probe archives.
- [ ] Repeat both suites; require pass. Commit the checkpoint boundary.

### Task 3: Quiescent Pi runner

**Files:** Create `pi-adapter.mjs`, `runner.mjs`, `package.json`, lock file, and `tests/harness/pi-adapter.test.mjs`, `runner.test.mjs`.

**Interfaces:** `loadPiRuntime({stateRoot}) -> Promise<{credentials,runtime}>`; `refreshCredentials({credentials,runtime,forceExpiry}) -> Promise<{refresh_succeeded,refresh_token_changed}>`; `runReadProbe({stateRoot,runtime,limits}) -> Promise<ProbeResult>`; `runJob(job, dependencies) -> Promise<JobResult>`. Job contains validated request, restore reference, generation and fixed limits. Result contains only phase/outcome, checkpoint descriptor, tool/answer booleans, usage counts and safe error enum.

- [ ] Pin Pi 0.99.2 and write failing tests with synthetic records. Import the real pinned AuthStorage/ModelRuntime APIs for storage behavior; replace only the remote inference/refresh boundary. Assert restored client ID and opaque fields survive; no API-key fallback; each request starts a new session; created host ID stays stable locally and outside archives; checkpoint runs only after session settlement. Assert three-turn/90-second/1,024-token bounds, aborted/error outcomes, unchanged credentials on local read, and no report/log exposure of a synthetic token marker.
- [ ] Run `npm ci --ignore-scripts --no-audit --no-fund --prefix components/agent-harness` and `node --test tests/harness/*.test.mjs`; identify behavior failures, not setup errors.
- [ ] Implement pinned SDK calls following the verified probe, with extensions/skills/context discovery and retries disabled for this acceptance request. Generate the fixture and prompt internally; do not accept an arbitrary shell command or prompt in this slice. Force refresh only for explicit acceptance input. Save a verified snapshot immediately after forced refresh, and another after settled inference; normal internal refresh remains covered only at request completion. Dispose the session before final snapshot. Write result atomically in protected local storage, and return only safe JSON; do not emit raw provider exceptions or conversation bodies.
- [ ] Inspect Pi host identity API usage and add the exact observed limitation to docs if refresh does not accept a per-host override. Do not patch OAuth provider semantics or merely claim the host-ID file was transmitted.
- [ ] Run all Node suites; require pass. Commit the runner boundary.

### Task 4: Cloud boundary and metadata verification

**Files:** Create `cloud.py`, isolated `requirements.txt`, `tests/test_sandbox_lifecycle_cloud.py`.

**Interfaces:** `Cloud.from_operator_profile(cfg: Config, profile: str) -> Cloud`; methods `assert_identity() -> None`, `create_key(name: str, expires_at: str) -> KeyHandle`, `remove_key(key_id: str) -> None`, `create_sandbox(attempt_id: str, key: KeyHandle) -> str`, `prepare(sandbox_id: str, key: KeyHandle) -> None`, `run_job(sandbox_id: str, job: dict, key: KeyHandle) -> dict`, `read_completed_result(sandbox_id: str, key: KeyHandle) -> dict|None`, `terminate_and_confirm(sandbox_id: str, key: KeyHandle) -> None`, `verify_checkpoint(ref: CheckpointRef) -> None`. `KeyHandle` keeps the value in memory and suppresses repr; only ID/expiry may be persisted.

- [ ] Write failing adapter tests using actual SDK request serialization with only transports replaced. Assert Singapore URL, existing Volume/role metadata, 900-second timeout, account mismatch before any writes, no credentials in uploaded job, and Node/Pi pins with hash verification. Test metadata checks reject missing objects, wrong size, prefix or non-AES256 encryption. Test ambiguous create/removal errors surface safe typed outcomes; do not map all network errors to not-found. Verify read_completed_result reads only the expected protected result file, never auth/session/log files.
- [ ] Run `python3 -m unittest discover -s tests -p test_sandbox_lifecycle_cloud.py -v`; confirm fail.
- [ ] Resolve/pin the new runtime dependencies independently of AgenticFS. Capture the existing official Aliyun CLI OAuth profile in memory, check STS account, and construct SDK clients without exporting secrets to child shell arguments or output. Use the successful Node/Pi bootstrap with bounded installation; upload reviewed harness files and lock file, not local secrets. API key expiry is at most one hour. Terminate by ID and confirm not-found without first calling connect/resume, which can extend sandbox lifetime. Verify both archive/checksum object metadata and the nonsecret checksum content; never fetch archive contents on the controller.
- [ ] Run adapter tests and dependency import/version checks. Commit the cloud boundary. A passing fake transport is not live cloud acceptance.

### Task 5: Run, recovery and cleanup state machine

**Files:** Create `lifecycle.py`, `tests/test_sandbox_lifecycle_lifecycle.py`.

**Interfaces:** `status(cfg: Config, ledger_path: Path, cloud: Cloud) -> dict`; `execute(cfg: Config, request: Request, ledger_path: Path, cloud: Cloud) -> dict`; `recover(cfg: Config, ledger_path: Path, cloud: Cloud) -> dict`. All use Task 1 ownership and Task 4 port. Controller results distinguish request, checkpoint and cleanup outcomes.

- [ ] Write failing tests through the real state machine/filesystem with a deterministic external cloud fixture. Assert pending intent is durable before key/sandbox creation; uncertain key/sandbox creation blocks replacement; failed restoration prevents inference; successful answer plus failed checkpoint retains the old reference and fails the lifecycle; refresh snapshot stays selected if later inference fails; unconfirmed termination retains sandbox/key IDs and blocks another run; cleanup key failure remains recoverable. Inject termination/create/save failures at each boundary. Assert recovery never replays a request, never guesses by filename, never erases checkpoints, and only adopts a complete descriptor after verification and confirmed old-run termination.
- [ ] Run `python3 -m unittest discover -s tests -p test_sandbox_lifecycle_lifecycle.py -v`; confirm failure.
- [ ] Implement the specified progression with safe enums. Persist IDs immediately after known create results. Publish only verified references atomically, keep request outcome distinct, and always attempt bounded cleanup after known failures. Recovery stops/verifies an old sandbox before admitting another. A pending-create outcome without known ID needs explicit SDK inventory reconciliation or operator resolution, never an automatic replacement; preserve the blocking ledger even if inventory support is unavailable. Unknown control-key creation is likewise reconciled by recorded unique name/expiry or left blocked until verified expired/removed.
- [ ] Run the state-machine suite, then all Python lifecycle suites and Node suites. Commit the orchestration boundary.

### Task 6: Usable CLI, offline CI and live acceptance

**Files:** Create `__main__.py`, `tests/test_sandbox_lifecycle_cli.py`, `.github/workflows/sandbox-lifecycle.yml`, `docs/setup/oss-sandbox-lifecycle.md`; update `README.md`. Copy the reviewed design/plan into repository docs at execution time with source links adjusted.

**Interfaces:** `main(argv: list[str]|None=None) -> int`. CLI: `python -m tools.sandbox_lifecycle {status,run,recover} --config PATH`, plus `--request PATH` for run and optional nonsecret `--profile` default `infra-ops-poc`. Fixed account state location follows Task 1. Status is default and never starts compute. Exit 0 only for a complete successful operation; 2 for validation/busy/blocked, 1 for an attempted run or recovery with unresolved failure.

- [ ] Write failing CLI tests: default status never creates compute; missing request/unknown fields fail; arbitrary ledger override cannot bypass account lock; synthetic provider secret marker absent from stdout/stderr; incomplete cleanup never returns success. Validate the exact safe output schema through all injected failures.
- [ ] Run the CLI suite; implement parser/report allowlist and operator documentation. Document one-time bootstrap checkpoint 5 import using ignored config, separate request JSON, canonical ledger, cleanup/recovery commands and the retained-token loss window. Explain Mac dependency, no cross-host locking, no queue and no infrastructure operations yet.
- [ ] Add offline CI with read-only repository permissions, pinned existing checkout action, Python 3.12, Node 22.23.3, isolated Python dependency install and npm ci. Run Python `unittest discover -s tests -p 'test_sandbox_lifecycle_*.py' -v`, Node harness tests and CLI help. No cloud secrets or live deployment in CI.
- [ ] Run the full existing Python test discovery as well as the new suites; the repository's Terraform fixture needs its verified binary/provider cache. Report any environmental failure explicitly rather than silently skipping it. Run Node tests and `git diff --check`. Check staged files for credential data; never search credential-bearing ignored directories into output. Commit and perform the selected independent review before PR/publication.
- [ ] Live preflight: refresh local operator credentials, verify account/Volume/bucket AES256 and scoped role; use metadata-only reads of retained checkpoint 5 and read only its checksum. Put actual IDs/reference in ignored protected config. Do not copy the real archive to the Mac or Git. Check current billed costs/balance as available; remaining balance alone is not total budget proof. If required resources or credentials fail validation, stop rather than widening permissions or creating alternatives.
- [ ] Run three short acceptance jobs using the actual reusable CLI: normal read probe; force-refresh read probe; normal read probe after replacement. Verify generated checkpoints/ledger references, exact answers, changed refresh token in the explicit acceptance report and absence of repeat interactive authorization. Confirm each old sandbox not-found before its replacement. Verify temporary-key cleanup and object encryption independently. Use real credentials only for positive tests; inject destructive failures offline with synthetic data.
- [ ] Save a nonsecret live acceptance receipt and update docs distinguishing offline/live evidence and limitations. Retain storage/checkpoints, clean temporary compute/control keys, and preserve ledger if any cleanup is uncertain. Create/attach the PR after review; merging remains a separate user action.

## Self-review and execution choice

Coverage: ownership/input constraints → Task 1; safe restore and immutable checkpoints → Task 2; Pi/request/refresh behavior → Task 3; SDK/account/encryption boundary → Task 4; uncertainty and recovery → Task 5; operator flow/CI/cloud evidence → Task 6. Each Review Focus input has an owning test task. Shared interfaces use the same CheckpointRef and safe JSON fields throughout.

The implementation reuses verified infrastructure and provider behavior without promoting the throwaway scripts directly into a product. No new always-on resource, browser-sign-in UI, distributed lock or infrastructure workflow is included. Request failure and checkpoint/cleanup failure stay separate, including successful inference followed by failed publication. Live acceptance is required before claiming this component works in Aliyun.

Recommended execution: **native**, because the six tasks depend closely on shared ledger/checkpoint interfaces. Implement sequentially in one isolated worktree and obtain an independent whole-branch review before publication. **Subagent-driven** remains available if the user prefers per-task independent implementation and review.
