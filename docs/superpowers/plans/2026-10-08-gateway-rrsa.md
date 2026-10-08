# Gateway RRSA Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans for inline execution. Steps use checkbox syntax for tracking.

**Goal:** Eliminate manual hourly gateway STS renewal using ACK RRSA, with both cloud SDKs refreshing automatically and real cross-refresh acceptance.

**Architecture:** One explicit OIDC provider with serialized cache access supplies atomic credentials to FC and OSS. Terraform owns component IAM and enables RRSA on the existing ACK; GitOps projects the exact ServiceAccount token and replaces the single gateway worker safely.

**Tech Stack:** Go 1.25, credentials-go 1.4.5, FC Sandbox SDK 1.6.0, OSS SDK 3.0.2, Terraform Alibaba provider, Helm, Argo CD.

**Spec:** design.md in this directory, approved by the user on 2026-10-08.

## Global Constraints

- Existing ACK: c92787e953503492ea141a744c81498f1, namespace raptor-system, ServiceAccount agent-gateway, audience sts.aliyuncs.com.
- Existing gateway role: raptor-rdev-gateway-controller; keep role identity and MaxSessionDuration=3600.
- Terraform imports require a no-change adoption plan before trust changes. No deletion/recreation of existing resources.
- No default credential chain, node role fallback, long-lived AK, scheduled restarts, request replay, or credential output/source/state storage.
- Preserve AX/Aliyun persistent provider bindings, AX mTLS and the existing worker lease.
- Check active executions, runtime slot and unresolved outcomes before deployment/credential changes. Use one gateway replica and Recreate.
- Retain encrypted checkpoint verification and exact archive/checksum object scope.

## Review Focus

- A projected-token symlink changes while refresh occurs: reopen the token file on each exchange.
- Concurrent FC and OSS calls during refresh: serialize SDK cache mutation and return atomic snapshots.
- STS failure or malformed/empty credentials: no cloud operation should leave the process with unusable credentials; sanitize errors.
- Terraform resources split across foundation and ecs-ax remote states: preserve identity and avoid a window where the old state can delete migrated resources.
- An accepted application.question has an unknown outcome: inspect existing request/attempt evidence instead of re-submitting.

## Task 1: Runtime credentials and signing

**Files:** create components/agent-gateway/internal/runtime/credentials.go and credentials_test.go; modify runtime/bootstrap.go and cmd/gateway/main.go. Preserve cmd/sandbox-compat/main.go static compatibility.

**Interfaces:** NewRRSACloudClients(c LiveConfig, roleARN, providerARN, tokenPath string) (Management, CheckpointVerifier, error); existing NewCloudClients(c LiveConfig, credential ControllerCredential) remains available. Shared provider implements credentials.Credential; OSS adapter implements oss.CredentialsProvider and oss.CredentialsProviderE.

- [ ] Write tests proving sequential actual FC and OSS requests carry generation A then generation B signatures/tokens, using local test transports; refreshing credentials must not reconstruct cloud clients.
- [ ] Write STS exchange tests using the actual OIDC provider with controlled HTTP interception: rotated projected token is sent on the next exchange; expiry triggers refresh; failed/invalid refresh prevents cloud HTTP calls; underlying errors and secrets do not escape adapter errors.
- [ ] Write concurrent shared-provider tests: cache access serialized; each returned snapshot contains matching AK/secret/token; race detector stays clean.
- [ ] Write mode tests: live rrsa starts without a controller credential file; static STS tooling still works; missing RRSA env, unknown mode, invalid config fail closed. Keep current no-autoretry behavior and run its existing tests.
- [ ] Run targeted tests and confirm RED for the missing new behavior before implementation.
- [ ] Implement explicit oidc_role_arn config and locked GetCredential access. Installed OIDC provider reopens token files and refreshes 180 seconds before expiry; it lacks a mutex, so the wrapper must supply one. Sanitize SDK errors and reject incomplete snapshots. Deprecated individual getters delegate through the wrapper; FC/OSS paths use atomic GetCredential snapshots.
- [ ] Wire FC Config.Credential and OSS SetCredentialsProvider with error-capable adapter; use fixed endpoints and existing guarded transports. Default existing static path remains compatible; rdev RRSA mode explicitly selects the new path.
- [ ] Run go test ./... and go test -race ./... from components/agent-gateway. Expected: all pass, no races. Commit runtime task.

## Task 2: Adopt component IAM without changing permissions

**Files:** create terraform-module/agent-gateway/{main,variables,outputs,versions}.tf and tests/permissions.tftest.hcl; create infra-terraform/environments/rdev.ali/agent-gateway/{main,variables,outputs,versions,backend}.tf; remove gateway checkpoint resources from terraform-module/ecs-ax/checkpoint.tf and update ecs-ax/tests/node.tftest.hcl after state migration.

**Interfaces:** module accepts account_id, role_name, team_id, volume_id, bucket, existing trust policy (adoption stage), RRSA provider ARN/issuer/subject (RRSA stage); outputs role ARN. Component owns both existing policy names and attachments. Roots only supply configuration and compose modules.

- [ ] Read current default versions for both attached policies; save non-secret identity/trust/permission evidence. Current controller policy has exactly Team Create/List/Update/DeleteApiKey, exact GetVolume, bucket GetBucketEncryption, auth/* GetObject; checkpoint policy has exactly PutObject on lifecycle/*.tgz and *.sha256.
- [ ] Write mock tests for exact action/resource sets, Custom attachments, role name and max session; RRSA trust rejects wrong subject/audience/provider. Observe RED before implementing module.
- [ ] Implement adoption configuration matching current role description/trust and both policy documents, with prevent_destroy protection. Verify provider schema/import IDs rather than inventing them.
- [ ] Initialize approved encrypted remote state using private backend config and environment credentials. Pull private state backups without printing their contents.
- [ ] Import manually owned role/controller policy/attachment. Migrate checkpoint policy/attachment from ecs-ax.ali state under locks; inspect IDs and serials before and after. Use a controlled remove/import transfer if cross-backend state move is unsupported, with no apply between removal and import. Disable stale ecs-ax applies during transfer.
- [ ] Verify adoption plan has 0 add/0 change/0 destroy and old ecs-ax plan cannot delete migrated resources. Run terraform test and validate. Commit module and migration documentation with sanitized evidence.

## Task 3: Enable RRSA through existing cluster owner

**Files:** terraform-module/rdev-foundation/main.tf, variables.tf, outputs.tf, tests/contract.tftest.hcl; infra-terraform/environments/rdev.ali/main.tf/variables.tf; agent-gateway trust config and tests from Task 2.

**Interfaces:** foundation exposes actual ACK RRSA metadata; gateway module receives exact issuer and provider ARN after activation. ACK provider-managed IdP is a shared prerequisite, never separately created by the gateway module.

- [ ] Inspect installed Alibaba provider schema/docs for existing-cluster RRSA support and update behavior; pin the supported attribute (enable_rrsa if confirmed). Test default-disabled and explicitly enabled configs without cluster replacement.
- [ ] Confirm remote foundation state resource ID matches the original ACK and obtain a clean baseline plan. Do not apply unrelated drift.
- [ ] Enable RRSA in Terraform. Inspect saved plan: only bounded existing-cluster RRSA update; no replacements or credential/certificate values in state. Apply saved plan, wait for running, read actual provider-managed IdP ARN/issuer.
- [ ] Change gateway role trust to only sts:AssumeRole (the RAM trust action for the AssumeRoleWithOIDC API) with exact Federated provider and StringEquals oidc:iss/oidc:aud/oidc:sub. Remove bootstrap AssumeRole trust after RRSA cutover plan is ready. Check any other users of this role before changing trust.
- [ ] Verify trust plan changes only intended trust document, no permission broadening or replacement. Apply saved plan and record resource IDs. Verify wrong-subject/audience tests and terraform tests pass. Commit infrastructure task.

## Task 4: GitOps RRSA deployment and worker safety

**Files:** helm-chart/raptor-platform/{values.yaml,templates/services.yaml,templates/serviceaccount.yaml}; scripts/rdev/test_platform_release.py; infra-kubernetes/environments/rdev.ali/values.yaml; docs/setup/gateway-rrsa.md.

**Interfaces:** gatewayLive.credentialMode=rrsa, gatewayLive.rrsa.roleARN/providerARN/tokenPath; static credentialMode continues controllerSecretName support. Projection audience sts.aliyuncs.com and 3600-second token expiry; reader UID/fsGroup 10001.

- [ ] Add Helm render tests for rrsa/static modes. RRSA must render dedicated SA, explicit projection, correct env/mount, Recreate, one replica, no controller secret validation/copy/mount. Reject incomplete RRSA or mixed modes. Verify unrelated workloads receive no RRSA projection and automount remains false. Observe RED.
- [ ] Implement conditional templates and defaults. Run real Helm renders and Python release suite. Expected: pass with no skipped rendering tests.
- [ ] Create implementation PR against latest main and attach it to this chat. Run required CI and review full diff. Preserve other chats' changes.
- [ ] Inspect database active execution/runtime slot/unresolved state through private authorized local access. Do not copy kubeconfig to worker. If busy, reconcile/drain before rollout and never submit duplicate inference.
- [ ] Merge reviewed PR under existing deployment authorization; wait for trusted-main image publication. Commit reviewed digest and RRSA values through established GitOps release process. Verify Argo main Synced/Healthy, gateway Ready and one worker lease/pod.
- [ ] Record deployed digest, pod UID, restart count, public credential generation timestamps/expiry only. Keep credential provider metadata endpoint/logging restricted and free of keys/tokens; choose existing private receipt channel if no safe observable metadata exists. Commit setup documentation and sanitized receipts.

## Task 5: Real cross-refresh acceptance and cleanup

**Files:** docs/setup/gateway-rrsa-execution-ledger.md; sanitized working execution receipt. Private test receipts remain .raptor-local.

**Interfaces:** ordinary-user application.question through deployed Raptor, gpt-5.6-luna, existing AX provider; existing private account receipt and E2E tooling.

- [ ] Verify private test account receipt without output, enable it only for acceptance, and use fresh private sessions.
- [ ] Submit one intended normal-user question; capture request ID and inspect its completion. Verify actual model, Raptor+Infra MCP calls, AES256 checkpoint export/readback, exact actor/task absence and request credential revocation. Unknown result is investigated using original request ID, not replayed.
- [ ] Keep same gateway pod/worker through a real STS expiry/refresh boundary. Record provider generation/expiry evidence; do not shorten this into a restart-based demonstration. Monitor quietly while unchanged and provide progress updates during ongoing work.
- [ ] Submit one second intended question after proven rotation; verify the same acceptance facts. Distinguish SDK/cloud signing rotation evidence from application inference evidence.
- [ ] Revoke every test session, disable account, confirm no active execution/slot/unresolved cleanup remains. Remove unused fixed controller Secret after proving no remaining consumer and successful RRSA acceptance.
- [ ] Final verification: gateway suite/race, Terraform no-change post-apply plan, Helm tests, Argo health and no-overlap worker, cross-refresh receipt and cleanup evidence. Report deployment and cross-refresh acceptance separately. Record any unachieved criteria explicitly.

## Self-review

All approved design requirements map to Tasks 1–5. IAM migration is sequenced before trust change; foundational RRSA is sequenced before exact issuer trust and GitOps rollout. Task 1 produces the runtime mode consumed by Task 4; Task 3 produces exact IdP values consumed by Tasks 2/4. Provider schema and live drift checks are evidence gates, not assumptions permitting speculative apply.

## Execution choice

Recommended: inline execution in this chat, with one independent final code review. Runtime, state adoption and release steps depend on each other's exact outcomes; per-task subagents would repeat context and need shared-state coordination.
