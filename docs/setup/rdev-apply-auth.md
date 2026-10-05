# Rdev deployment identity

Created 2026-10-05 and independently read back. Role: raptor-iap-rdev-apply. Trust binds issuer token.actions.githubusercontent.com, audience sts.aliyuncs.com and exact immutable subject repo:jamesDeng@4443650/raptor-iap@1397422754:environment:rdev.ali-apply. Maximum session duration is 3600 seconds; check workflow requests 1800.

GitHub environment rdev.ali-apply permits main only and requires jamesDeng approval. Admin bypass is disabled. Self-review remains permitted because the owner is the POC operator. Repository/environment protection is part of the trust boundary; the OIDC subject does not itself contain a branch claim.

Attached policies: existing raptor-iap-rdev-plan-read metadata policy, and raptor-iap-rdev-apply-state. The latter reads/writes only the exact rdev state object and accesses shared terraform_lock rows. Shared lock risk was explicitly accepted for this POC. No cloud creation policy is attached. State contents may contain secrets and must never be printed or uploaded as public artifacts.

The manually dispatched rdev-apply-oidc-check workflow runs only on main. It verifies temporary credentials through account discovery without provisioning infrastructure. Environment approval is required before its job starts. Until it runs successfully, live OIDC exchange for this role is unverified. The check does not verify OSS state access or OTS row locking; those remain a separate live check.

The initial creation policy was accepted by native RAM CreatePolicy and independently read back with zero attachments. Numeric condition values must use string encoding. This is policy acceptance, not live creation authorization or an assurance that all provider follow-up calls will succeed. Cloud creation awaits preflight and saved-plan review.

NAT deletion protection scope was resolved using the official AliyunNATGatewayFullAccess policy and live GetPolicyVersion readback: vpc:DeletionProtection authorizes natgateway resources. Future follow-up policy should use the actual NAT ID. Source: https://help.aliyun.com/zh/ram/developer-reference/aliyunnatgatewayfullaccess .

## First-time state check

The identity workflow also uses pinned Terraform to initialize/read the exact rdev backend, inspect a refresh-only saved plan, and persist only empty/data-only state. It refuses existing managed resources and never provisions foundation infrastructure. Plans/state/errors remain in a restricted temporary directory and are removed at completion. Normal backend locking runs during plan/apply; a separate contention test is not claimed. All future environment jobs must share concurrency group rdev.ali-state.

This is a first-time bootstrap check: after foundation deployment, it intentionally fails rather than using a data-only fixture against populated state. Remove/replace that step with a populated-state read check at that stage.

Review fixes: inspect all saved-plan sections recursively for managed resources, including prior state and drift. Require a fresh UUID output marker in each saved refresh-only plan and verify that marker in remote state after apply, so reruns must prove an actual state write. This writes state metadata only and makes no cloud infrastructure change.

## Protected initial foundation plan

`rdev-live-plan.yml` is a manual, main-only job behind `rdev.ali-apply`. It shares the `rdev.ali-state` concurrency group with state verification. It obtains temporary OIDC credentials for the apply role, generates a private plan, and checks the reviewed creation-only shape: 10 cloud resources and two local guards, the selected worker/RDS sizes, private API/database settings and protection settings. It performs no apply and publishes only a sanitized summary and source SHA. Raw state, plan and provider logs remain temporary and are removed when the runner exits.

The initial guard intentionally rejects updates, replacements, deletes, incomplete/deferred plans, drift and unexpected managed resources. After partial provisioning, this job will reject the changed plan: reconcile retained state and build a separately reviewed continuation rather than bypassing it. A passing plan proves the role can read and use state locking; it does not verify cloud creation authorization or runtime health.

The owner approved the initial creation/read/follow-up policies on 2026-10-05. Independent RAM readback verified exactly five Custom policy attachments (the two existing read/state policies plus those three), retaining explicit kubeconfig/attach-script/trigger denies. The bootstrap follow-up grant temporarily covers account resources in Singapore for tagging/protection/DAS changes, including protection disabling. Narrow it to owned IDs after creation. RDS CreateDBInstance restricts engine/version/class/storage but does not have a verified region IAM condition. No resource deletion, resizing, broad PassRole or RAM administration was added.

## Controlled initial apply

`rdev-foundation-apply.yml` is manual, main-only and protected by rdev.ali-apply; it shares the state concurrency group. Required input expected_source_sha must exactly equal the checked-out commit. Temporary OIDC session requests3600seconds within the verified role maximum. Current public sanitized preflight evidence must be at most6hours old, stock available, full planned estimate within150CNY, balance sufficient and reported prior billing plus estimate below1000CNY. Unverified inventory requires explicit owner acceptance of the bounded estimate/reserve; the current pending approval flag is false.

The runner creates a fresh private plan, checks provider/source/region, resource counts, selected settings, Flannel/pod/service networks, and parent references to newly created environment resources. It rejects existing parent IDs and malformed plan entries. Only that saved plan is applied once. No automatic retry or destroy occurs. State remains in encrypted/versioned OSS after failure; private runner files expire with the job. No public raw artifacts. Post-apply state presence is checked but is not independent cloud health or application acceptance.

An apply failure may leave billable resources. Reconcile cloud IDs and remote state before any continuation, generate a new reviewed continuation plan, and narrow temporary bootstrap policies to owned IDs. The initial creation-only guard intentionally cannot continue a partially provisioned state. The successful run records a72hour review deadline; no automatic teardown or reminder is installed. Provider deletion protection is enabled, and later teardown needs explicit authorization and an owned-resource policy.

### Recovery and elapsed-time limits

Apply uses file-backed private diagnostics and a45minute monotonic execution budget. Slow preparation refuses to start apply without at least25minutes remaining. Each operation reserves5minutes for interruption/recovery; timeout sends one SIGINT and waits up to120seconds, with no forced kill or retry. If Terraform still has not stopped, cleanup is skipped; the encrypted snapshot may be incomplete and cloud inventory/state reconciliation is mandatory. The55minute job and3600second credential session leave recovery/upload headroom. A hung provider or runner termination can still leave uncertain cloud outcomes; no atomic creation guarantee is claimed.

A reviewed RSA3072 public key is committed; its private key stays only in the local ignored recovery directory, mode600. AES256-GCM encrypts a ZIP of available state/recovery files and provider logs, and RSA-OAEP-SHA256 wraps its random encryption key. GitHub stores only this ciphertext for14days, including failed runs. Working files are removed only after an actual artifact upload succeeds and Terraform is no longer stopping. Artifact service failure or lost private key can defeat recovery; preserve the local key separately before relying on this channel. Raw state is never a public artifact. The tested decrypt_bundle helper restores into a private directory and rejects paths outside it.
