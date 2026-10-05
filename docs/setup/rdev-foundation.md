# rdev.ali foundation

Status: preparing; no ACK/RDS provisioned. Module tests use mocks and do not prove deployment.

The approved configuration is one ACK Basic cluster, one ecs.e-c1m2.xlarge worker and RDS PostgreSQL Basic pg.n2e.1c.1m with 10 GB general_essd storage. Deletion protection is enabled. Cloud apply is pending complete pricing/permissions, version verification, remote state and source integration.

Run the offline preflight checker with a private JSON receipt: `python3 scripts/rdev/preflight.py /private/path/evidence.json`. It rejects unknown costs, absent stock, unintegrated source, invalid credentials or a forecast above RMB150. It consumes verified evidence; it is not a credential/price discovery service or a provider spending cap.

Terraform environment lives in infra-terraform/environments/rdev.ali. Do not apply the module directly. Backend and gateway migrations and credentials are independently owned. Public source must never contain DB passwords or kubeconfig.

Raptor image contains frontend/backend/open-api/admin binaries: choose the entry point explicitly for each deployment. Gateway has its own image. Both run as non-root; container environment must set bind addresses to 0.0.0.0 because local-development defaults bind loopback. Pi and Infra API remain in external sandbox and serverless runtimes.

PR5 and PR6 were open at implementation start. Foundation branch based on PR6 is for preparation only until release integration is authorized.
