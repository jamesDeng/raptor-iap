# rdev Infra API deployment discovery

This package connects the existing Go read service to the private rdev ACK API. Deployment is not complete until the live acceptance receipt confirms actual cluster UIDs and readiness.

## Configuration

Serverless Devs uses `components/infra-api/s.rdev.yaml`, pinned FC3 component 0.1.25, explicit Singapore VPC/subnet/security group and an IAM-protected GET trigger. Concurrency is capped at two, with no provisioned warm instances. The existing security-group ID is supplied as INFRA_SECURITY_GROUP_ID and shared-gateway ID as Terraform gateway_instance_id, privately after ownership verification. Runtime caller credentials and scope are generated privately, never committed. Terraform owns the dedicated read/invocation roles and three GET routes on the existing shared gateway. Its encrypted private OSS state uses a distinct `rdev.ali/infra-api.tfstate` key.

The function execution role permits STS identity and kubeconfig retrieval for ACK c92787e953503492ea141a744c81498f1 only. The existing FC service-linked role supplies platform network access; no ENI mutation permission is added to the application's role. See [FC role documentation](https://help.aliyun.com/en/functioncompute/grant-function-compute-permissions-to-access-other-alibaba-cloud-services).

ACK requires a separate custom ClusterRole granting only get/list on Deployments, ReplicaSets and Pods. Bind the new role's verified numeric RoleId, with no Secrets, logs, exec or mutation privileges. See [ACK RBAC documentation](https://help.aliyun.com/en/ack/ack-managed-and-ack-dedicated/user-guide/grant-rbac-permissions-to-ram-users-or-ram-roles).

## Raptor wiring

Only raptor-backend references the optional `infraReader.secretName` Secret. That Secret contains INFRA_USERNAME and INFRA_PASSWORD; the dedicated X-Infra-Authorization header survives API Gateway's Authorization handling. Do not replace the existing database/auth Secret.

Update the existing rdev.ali environment with clusterId and https://infra-api.raptor-iap.top. Validate before/after using scripts/rdev/infra_discovery_bootstrap.py, preserving all unrelated fields and existing object codes. Do not rerun catalog seed or database initialization.

## Acceptance

Verify protected direct-trigger denial, missing/wrong caller credential denial, identity account/region, each of five catalog codes against actual ACK namespace/UID and pod readiness, and public signed-in Raptor discovery. Provider failure must remain unavailable, never successful empty discovery. No restart is part of this deployment.

Revoke the temporary function deployment credential after acceptance. Retain runtime/invocation roles and backend caller Secret. Agent execution and database/proxy acceptance remain separate unfinished POC milestones.
