# Gateway RRSA

The gateway can use `GATEWAY_CONTROLLER_CREDENTIAL_MODE=rrsa` with explicit `ALIBABA_CLOUD_ROLE_ARN`, `ALIBABA_CLOUD_OIDC_PROVIDER_ARN`, and `ALIBABA_CLOUD_OIDC_TOKEN_FILE`. Both FC and OSS share the same serialized credential provider. It reads projected token rotations and exchanges STS credentials automatically before expiry. Refresh errors stop cloud signing; application questions and cloud mutation requests are never replayed by the refresh layer. Mode `static-sts` (or omitted for compatibility tooling) reads the existing private STS file. RRSA rejects a simultaneously configured static file.

The Helm chart creates ServiceAccount `raptor-system/agent-gateway` and explicitly projects audience `sts.aliyuncs.com` at `/run/raptor/rrsa/token`. Only the gateway receives it; namespace-wide injection is unnecessary. One replica with Recreate and the existing DB lease protect against overlapping workers. Keep the private config and AX mTLS Secret; remove the static controller Secret reference in RRSA values.

## Terraform ownership

`terraform-module/agent-gateway` owns the controller role, controller policy, checkpoint policy and both Custom attachments. Environment root `infra-terraform/environments/rdev.ali/agent-gateway` stores encrypted, locked state under `gateway.ali/terraform.tfstate`. The original cluster belongs to `terraform-module/rdev-foundation`, configured with `enable_rrsa`; its `rrsa_metadata` output provides the exact issuer and provider ARN. ACK creates/manages the cluster OIDC provider as a shared prerequisite; the gateway module never creates another IdP.

Adoption starts with `rrsa_enabled=false`, matching the existing trust and permissions. Import role/policy by existing name and attachments by `role:<policy_name>:Custom:<role_name>`. Move checkpoint policy/attachment ownership out of ECS-AX state before applying anything; preserve private backups and IDs, use state locks, and freeze applies against stale ECS-AX definitions. This branch removes those definitions. Verify a no-change gateway adoption plan before setting RRSA trust; never delete/recreate these resources.

After foundation activation, pass actual `oidc_provider_arn` and `oidc_issuer`, and set `rrsa_enabled=true`. The trust permits only exact original ACK IdP/issuer, namespace/ServiceAccount and audience. Bootstrap root AssumeRole trust is removed. Account guard and prevent_destroy protect the existing component identity. No provisioning credentials are chart values or Terraform variables; use temporary execution credentials in the environment.

## Acceptance

Before any rollout/trust cutover, check active executions, runtime slot and unresolved outcomes; drain/reconcile before replacing the worker. Follow trusted-main image publication and GitOps digest updates. A Ready pod proves deployment only. Record unchanged pod UID/restart count and actual credential generations across a real STS refresh/expiry boundary, then demonstrate ordinary-user gpt-5.6-luna questions, both MCPs, AES256 checkpoint export/readback, actor/task cleanup and credential revocation. Unknown submission outcomes require investigating the original request, never re-submitting it. Revoke all test sessions and disable the test account afterward.

Tests: gateway `go test -race ./...` with a disposable test DB; gateway/foundation Terraform mock tests; `scripts/rdev/test_platform_release.py` with Helm. Installed credentials-go1.4.5 has an unsynchronized OIDC cache and dereferences missing Expiration; the wrapper serializes access and contains malformed-response panics as sanitized dependency failures.

Official references: [ACK RRSA](https://www.alibabacloud.com/help/en/ack/ack-managed-and-ack-dedicated/user-guide/use-rrsa-to-authorize-pods-to-access-different-cloud-services), [RAM attachment import](https://registry.terraform.io/providers/aliyun/alicloud/latest/docs/resources/ram_role_policy_attachment.html).
