# Read integration acceptance — 2026-10-05

Status: live sandbox → HTTPS API Gateway → protected FC → Aliyun STS acceptance passed. Temporary probe resources have been removed; the hostname is not an active service endpoint now.

The [sanitized live receipt](infra-api-read-live-receipt.json) records both local and real Singapore sandbox results. Correct caller credentials returned the exact live identity envelope with account match. Missing and incorrect caller credentials returned 401. A deliberately invalid, correctly shaped ACS3 signature sent directly to FC returned 403 with `InvalidAccessKeyID`, establishing rejection at the FC IAM gate rather than the application.

Serverless Devs 3.1.10/fc3 0.1.25 deployed the static Go HTTP function with 512 MB, a 30-second timeout and a GET-only `authType: function` trigger. Terraform 1.13.3/alicloud 1.293.0 created the separate read/invocation roles and three routes on the pre-existing VPC_SHARED gateway. The owned `infra-api.raptor-iap.top` hostname used the issued DigiCert certificate with normal TLS validation.

The existing operator OAuth-derived STS credentials were rejected by FC in both tested clients. A dedicated RAM user credential succeeded. Its read permission was restricted to the exact POC function: that function returned 404 before creation, while another function returned 403. Deployment permissions were then added for that function and passing its existing read execution role, with a two-hour policy expiry. The credential, user and custom policy were deleted after testing. This observation does not establish that FC generally lacks STS support.

Two live integration issues were corrected with failing-then-passing regressions: API Gateway pass-through mode rejects declared header mappings, and FC3 uses a native `Code` field plus ACS3 request framing for the negative IAM test. Three probe tests and two Terraform contract runs pass. The project's pinned E2B 2.31.0 SDK created the sandbox; the newer 2.52.0 creation path returned 405. Inventory reconciliation confirmed no orphan before retrying.

Cleanup receipts confirm Serverless Devs function removal, Terraform removal of the three APIs/group and two roles/policies/attachments, gateway domain unbinding, and deletion of the created CNAME. The successful sandbox was terminated and its temporary API key deleted. The purchased domain, issued certificate, existing shared gateway, storage and OAuth checkpoints remain. Exact resource IDs and private logs remain in ignored local execution inventory.

The final billing snapshot shows CNY986 available and CNY14 billed for September–October, including the domain purchase. Delayed charges remain possible; this is not a hard spending limit.

Remaining acceptance: actual ACK workload discovery/status and Kubernetes RBAC, tagged RDS/ESS inventory, runtime mutations and zero failed application operations during PgCat replacement. This slice only proves authenticated live reads and ingress; it does not prove those later POC cases.

Earlier local validation covered all three Go race/vet suites, 114 legacy Python tests, 25 harness tests, static Linux packaging, pinned FC3 schema and Terraform validation. Those runs were recorded before cloud deployment; no Go service behavior changed during the ingress/probe fixes.
