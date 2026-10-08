# Gateway ACK RRSA design — 2026-10-08

Status: proposed implementation design; cloud inspection complete, no deployment changes made.

## Verified baseline

A fresh origin/main fetch resolved to c024b9cc09eec7383b1a7755dbff9e35504cee61. The repository checkout is on docs/go-platform-plan and the AX worktree belongs to a separate branch; neither should be modified for this task.

Read-only ACK GET /clusters/c92787e953503492ea141a744c81498f1 returned state=running and parsed meta_data.RRSAConfig.enabled=false on 2026-10-08. RRSA therefore needs enabling through the Terraform owner of that existing cluster. Do not create a second cluster or duplicate its provider-managed OIDC identity provider.

Current runtime/bootstrap.go supplies fixed AK/secret/token to both FC OpenAPI and OSS. credentials-go v1.4.5 is already present. OSS v3.0.2 supports CredentialsProviderE, including propagation of GetCredentialsE errors in signing paths. The Helm deployment currently requires and copies a fixed controller Secret, has no explicit gateway ServiceAccount, and defaults to a rolling deployment.

## Selected approach

Use explicit oidc_role_arn credentials, never a default credential chain or node role fallback. A single shared provider exchanges the projected ServiceAccount token with STS and refreshes credentials before expiry. FC receives the provider through OpenAPI Config.Credential. An OSS adapter returns one atomic AK/secret/token snapshot through GetCredentialsE and propagates sanitized errors. Verify the SDK cache and concurrency behavior with its actual source before deciding whether a wrapper lock is needed. Reads of projected tokens must reopen the path on refresh so kubelet rotation is observed.

Keep an explicit static STS mode only for existing compatibility tooling; live rdev gateway selects RRSA and stops mounting its controller credential Secret. No scheduled restart, long-lived AK, credential logging, or credentials in Terraform state. Authentication mode errors fail at startup. Cloud credential refresh does not replay application.question or introduce new cloud operation retries; preserve existing no-retry SDK options and persistent provider binding.

## Terraform ownership and adoption

Add a gateway component IAM module owning the existing role, existing minimum-permission policies, and attachments. Inspect actual role documents and attachments, then import existing resources into their matching module addresses and obtain a no-change plan before changing trust. Move the existing ecs-ax checkpoint policy and attachment state into this module without replacement, preserving their names and object-scoped PutObject resources.

The ACK cluster owner enables RRSA with the provider-supported Terraform attribute. ACK manages its OIDC IdP as a documented shared prerequisite. Read the resulting actual issuer/provider ARN after activation. Restrict trust to that exact provider/issuer, audience sts.aliyuncs.com and subject system:serviceaccount:raptor-system:agent-gateway. Preserve Team API key management/Volume reads, OSS encryption/auth reads, and auth/lifecycle archive/checksum writes exactly as deployed. Environment roots only compose modules and supply verified values.

## GitOps deployment

Add a dedicated agent-gateway ServiceAccount and explicitly projected token with audience sts.aliyuncs.com. Configure role ARN, IdP ARN and token path in the gateway environment. Use explicit projection instead of namespace-wide webhook injection to avoid changing permissions of other workloads. Retain automountServiceAccountToken=false; projected RRSA token is separate. Ensure UID 10001 can read the projection. RRSA Helm mode conditionally omits controller Secret validation, mounts and copy steps. Gateway uses Recreate with one replica to prevent overlapping workers.

Before rollout check active executions, runtime slot and unresolved outcomes; drain safely before replacing the worker. Keep AX/Aliyun bindings and AX mTLS. Merge/release through the repository's established reviewed image and Argo process. Remove the unused live fixed-ST​S secret only after successful RRSA deployment and cleanup verification.

## Verification and release evidence

1. Tests prove FC and OSS signing each observes changed credentials, a refresh failure sends no request, token rotation is read, and concurrent refresh requests are safe. Run gateway full suite and race tests; verify cloud mutations are not replayed.
2. Terraform adoption no-change plan, subsequent bounded RRSA/trust plan, and state migration show no resource replacement. Render/test Helm static and RRSA modes.
3. Record deployed image, pod UID/restart count and credential generation timestamps without any tokens or keys. Prove actual STS rotation across a real expiry/refresh cycle while pod identity remains stable.
4. One normal-user application.question per intended trial, using gpt-5.6-luna, produces Raptor and Infra MCP evidence, AES256 checkpoint export/readback, actor/task cleanup and request credential revocation. Unknown outcomes require inspection, never blind resubmission.
5. Revoke all test sessions and disable the private test account. Keep private receipts outside tracked files. Report deployment success separately from cross-refresh acceptance; neither implies the other.

## Sources

- Current task handoff: required Terraform/GitOps ownership, minimum permissions and acceptance criteria; authoritative task scope, not deployment evidence.
- ACK API inspection described above: independently verified RRSA disabled.
- Repository at c024b9c: components/agent-gateway/internal/runtime/bootstrap.go; cmd/gateway/main.go; helm-chart/raptor-platform/templates/services.yaml; terraform-module/ecs-ax/checkpoint.tf.
- Installed SDK source: github.com/aliyun/aliyun-oss-go-sdk@v3.0.2+incompatible/oss/conf.go and conn.go; github.com/aliyun/credentials-go@v1.4.5/credentials/providers/oidc.go.
- Official ACK RRSA documentation: https://www.alibabacloud.com/help/en/ack/ack-managed-and-ack-dedicated/user-guide/use-rrsa-to-authorize-pods-to-access-different-cloud-services
