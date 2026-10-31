# rdev Infra API deployment discovery

This package connects the existing Go read service to the private rdev ACK API. Deployment is not complete until the live acceptance receipt confirms actual cluster UIDs and readiness.

## Configuration

Serverless Devs uses `components/infra-api/s.rdev.yaml`, pinned FC3 component 0.1.25, explicit Singapore VPC/subnet/security group and an IAM-protected GET/POST trigger. Concurrency is capped at two, with no provisioned warm instances. The existing security-group ID is supplied as INFRA_SECURITY_GROUP_ID and shared-gateway ID as Terraform gateway_instance_id, privately after ownership verification. Runtime caller/service/inspector credentials are injected privately, never committed. Runtime authority comes from fresh catalog context and provider resolution, with no local scope file. Terraform owns dedicated read/invocation roles and Gateway routes on the existing shared gateway; the retained identity GET route is legacy and is not implemented by the active handler. See the [current endpoint inventory](infra-api-resource-endpoints.md). Its encrypted private OSS state uses a distinct `rdev.ali/infra-api.tfstate` key.

The function execution role permits STS identity and kubeconfig retrieval for ACK c92787e953503492ea141a744c81498f1 only. The existing FC service-linked role supplies platform network access; no ENI mutation permission is added to the application's role. See [FC role documentation](https://help.aliyun.com/en/functioncompute/grant-function-compute-permissions-to-access-other-alibaba-cloud-services).

ACK requires a separate custom ClusterRole granting only get/list on Deployments, ReplicaSets and Pods. Bind the new role's verified numeric RoleId, with no Secrets, logs, exec or mutation privileges. See [ACK RBAC documentation](https://help.aliyun.com/en/ack/ack-managed-and-ack-dedicated/user-guide/grant-rbac-permissions-to-ram-users-or-ram-roles).

## Raptor wiring

Only raptor-backend references the optional `infraReader.secretName` Secret. That Secret contains INFRA_USERNAME and INFRA_PASSWORD; the dedicated X-Infra-Authorization header survives API Gateway's Authorization handling. Do not replace the existing database/auth Secret.

The trusted environment `infraApiUrl` must identify the coordinated HTTPS Infra service. Infra requires fixed HTTPS `RAPTOR_CATALOG_ORIGIN` and existing `RAPTOR_SERVICE_USERNAME`/`RAPTOR_SERVICE_PASSWORD` for catalog-only `/v1/infra/resource-context` reads. Independently verify catalog `cloud=aliyun`, Singapore region, account and consistent structured ACK cluster identity; preserve unrelated fields and existing canonical object codes. Proxy admission also requires `proxyApplications` mapping the canonical proxy to exactly one canonical application and exactly one resolved Deployment. These are required inputs, not values populated by this source change. Do not rerun catalog seed or database initialization. Raptor obtains private proof through the existing deployments route with the proof Accept representation, not the local standalone proof path.

## Acceptance

After coordinated release prerequisites pass, verify protected direct-trigger denial, missing/wrong/revoked/foreign credential denial, logical typed deployment/status output against actual ACK namespace/UID/readiness, and public signed-in Raptor discovery. Account/region checks are internal to provider resolution; the removed identity endpoint cannot establish acceptance. Use the existing logical `go run ./cmd/mcp-probe` with private configuration described in the [read guide](infra-api-read.md). Provider failure must remain unavailable, never successful empty discovery. No restart is part of this deployment.

Revoke the temporary function deployment credential after acceptance. Retain runtime/invocation roles and backend caller Secret. Agent execution and database/proxy acceptance remain separate unfinished POC milestones.

Source-only status: source tests/builds do not prove this package is deployed. Release still requires matching reviewed Raptor/Gateway/harness/guest/FC receipts, verified catalog relationships, effective permissions and replacement disabled/idle readiness. The inspected FC policy lacks `nlb:ListServerGroups` (exact owned servergroup ARNs) and `nlb:ListListeners` (account/region `listener/*`); a reviewed component Terraform policy change and sanitized PR plan comment/reviewed apply are required, with no grant included here. Read-only qualification must establish fresh workload/metrics, baseline, harmless approval/pause/resume and owned cleanup. A compatible immutable replacement skill release and native closure remain separate gates; full 2→4→2 is NOT PASS.
