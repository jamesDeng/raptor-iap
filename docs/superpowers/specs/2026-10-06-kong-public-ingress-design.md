# Kong public ingress for rdev.ali

Status: proposed deployment addendum; user selected public access. Kong is not installed.

## Intent and boundaries

Provide public HTTPS browser access to Raptor and authenticated HTTP/MCP access for agents. Kong Gateway handles incoming traffic; Kong Ingress Controller reconciles Kubernetes routing. The existing agent-gateway retains its execution/progress responsibilities.

Use the existing Singapore ACK worker. Install one Kong Gateway replica in DB-less mode and one controller replica through the official kong/ingress Helm chart, pinned to verified compatible versions. Manage release values and routes through Argo CD. No Kong PostgreSQL database or hosted Konnect dependency.

## Public surface

Proposed hosts under the previously verified owned raptor-iap.top domain:
- raptor.rdev.raptor-iap.top: Raptor frontend, which uses its existing backend proxy.
- api.rdev.raptor-iap.top: authenticated Raptor open-api HTTP/MCP service.

Keep Raptor admin, agent-gateway, backend direct access, Argo CD, Kong Admin API and Kubernetes API private. Verify the frontend proxy's accessible paths and authentication before enabling its public route. Existing application authentication remains mandatory; Kong does not grant application access.

Expose a single Kong proxy through one Internet-facing Alibaba Cloud load balancer. Prefer a Terraform-owned, dedicated CLB with TCP forwarding to Kong, subject to live Singapore availability, supported annotations and actual price. Bind the Kubernetes Service to its exact ID; never reuse an unrelated load balancer or let GitOps create an unreviewed one. Assess NLB only if CLB compatibility or live pricing makes it preferable.

Terminate HTTPS at Kong. Create DNS records for the actual public endpoint. Use a valid certificate covering the two exact names; the earlier Infra API certificate is not evidence of coverage. Keep private keys in Kubernetes Secrets, never in Git or rendered public artifacts. Port 80, if needed, only redirects to HTTPS; credentials must never traverse plaintext HTTP.

## Verification and cost gates

Before provisioning, verify owned DNS zone, certificate coverage/expiry, current worker capacity, existing CCM/controller compatibility and Singapore load balancer price. No quoted cost is currently verified. Include load balancer and traffic charges in the existing below-RMB1000 POC budget.

Review controller RBAC and Secret access before applying. Any necessary new IAM/security-sensitive access grant is a separate concrete action-time confirmation; the standard Argo approval does not authorize Kong permissions.

Accept only after controller and proxy are Ready, Argo reports the pinned release healthy, DNS resolves to the owned load balancer, TLS hostname verification passes, browser login works, unauthenticated API/MCP requests are rejected, and private management endpoints have no public routes. Do not expose incomplete agent execution as successful.

## Dependencies and ordering

Continue the existing private PostgreSQL/bootstrap deployment after its pending approval. Prepare ingress packaging alongside it. Publish public routes only after platform authentication and backend proxy behavior are verified. Save exact deployed versions, Git revision, resource IDs and sanitized test results.

## Sources

- Current user instruction: use Kong and Kong Ingress Controller on ACK; public access first.
- Existing platform design: working/design/2026-10-06-rdev-platform-deployment-design.md. This addendum changes its private-only browser access constraint, not internal management access.
- Owned domain historical verification: docs/setup/infra-api-read.md and infra-api-read-live-receipt.json in the implementation repository. Live ownership/coverage must be rechecked.
- https://developer.konghq.com/kubernetes-ingress-controller/install/
- https://charts.konghq.com/
- https://www.alibabacloud.com/help/en/ack/serverless-kubernetes/user-guide/use-annotations-to-configure-load-balancing
