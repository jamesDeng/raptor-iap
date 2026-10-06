# Kong public ingress implementation plan

> For agentic workers: use superpowers:executing-plans inline with one final independent review. Preserve the user's selected native execution method.

Goal: public, authenticated Raptor access through Kong on the existing rdev.ali ACK cluster.
Architecture: Terraform owns a dedicated public load balancer and DNS; Argo CD reconciles pinned Kong Gateway/KIC and application routes. Kong runs DB-less; existing platform database bootstrap remains a separate dependency.
Tech stack: Kong official ingress Helm chart, Kubernetes networking.k8s.io/v1 Ingress, Argo CD, Terraform/alicloud, existing Go services.
Spec: working/design/2026-10-06-kong-public-ingress-design.md, approved by user October6.

## Global constraints
- Existing Singapore ACK worker; no additional worker or Kong database.
- One Gateway replica and one controller replica; no Konnect dependency.
- Public hosts: raptor.rdev.raptor-iap.top and api.rdev.raptor-iap.top.
- Admin, direct backend, agent-gateway, Argo, Kong management and Kubernetes API remain private.
- TLS private keys and credentials never enter Git, public artifacts or printed output.
- One dedicated Terraform-owned load balancer; reuse only its exact ID.
- Include all new charges in the below-RMB1000 POC budget; verify live price before creation.
- Review any new security-sensitive permission grant separately at action time.

## Review focus
1. Public frontend proxy may forward administrative paths: verify application authorization and deny sensitive management paths at ingress.
2. Chart defaults may expose Admin API or automatically create an extra load balancer: render and assert exactly the intended public proxy Service.
3. Existing certificate may not cover proposed names: validate SANs and expiry before route publication.
4. Authentication cookies may behave differently behind TLS termination: verify Secure cookies, forwarded scheme handling and CSRF behavior.
5. GitOps drift or a mismatched load balancer ID may affect unrelated infrastructure: fail closed on ownership and Terraform action scope.

## Task 1: Pin installation and prepare public-surface contracts
Files: helm-chart/kong-ingress/{Chart.yaml,Chart.lock,values.yaml}; scripts/rdev/test_kong_ingress.py; docs/setup/rdev-kong.md.
Consumes: official chart metadata and current cluster/CCM versions.
Produces: checked chart lock and rendered installation contract.
- [ ] Inspect official chart and compatibility tables; record exact chart, Gateway and controller versions, artifact hashes and licenses. Do not infer compatibility from latest tags.
- [ ] Write failing rendered-manifest tests asserting DB-less mode, one replica each, no public Admin API, no Konnect credentials, explicit resources, exactly one proxy LoadBalancer Service and exact-ID annotation required.
- [ ] Render the missing wrapper to observe failure, then implement the smallest wrapper and pinned dependency.
- [ ] Run Helm lint/template plus contracts; inspect controller RBAC and namespace/Secret watch scope. Save the concrete grant description for any required confirmation.
- [ ] Commit packaging and documentation.

## Task 2: Owned load balancer and DNS provisioning
Files: terraform-module/kong-ingress/{main.tf,variables.tf,outputs.tf}; infra-terraform/environments/rdev.ali/{main.tf,variables.tf,outputs.tf}; scripts/rdev/kong_plan_guard.py; scripts/rdev/test_kong_plan_guard.py.
Consumes: exact owned VPC, Singapore zone/subnet, DNS zone and live price quote.
Produces: owned load balancer ID/public address plus DNS outputs.
- [ ] Read current DNS/certificate inventory, balance, CLB/NLB availability and live cost. Choose the approved CLB preference if supported and affordable; record any evidence-backed deviation before provisioning.
- [ ] Write failing plan-guard tests: reject existing-resource updates/deletes, foreign account/region/VPC, unrelated DNS names, multiple load balancers and unverified cost receipt.
- [ ] Implement Terraform module for one dedicated Internet-facing load balancer and the two exact DNS names. Let ACK manage Service listeners/backend groups; avoid Terraform/CCM competing ownership of those fields.
- [ ] Implement validate_plan(plan: dict, ownership: dict, cost_receipt: dict) -> dict in kong_plan_guard.py; run tests RED to GREEN and Terraform validation/mocks.
- [ ] Commit, review a fresh plan and price, then apply only the guarded artifact under existing deployment authorization. Any additional IAM grants require separate exact confirmation.

## Task 3: HTTPS routes and Argo integration
Files: infra-kubernetes/environments/rdev.ali/kong/{application.yaml,project.yaml,values.yaml,routes.yaml}; scripts/rdev/test_kong_ingress.py; relevant frontend/auth Go tests only if behavior needs correction.
Consumes: Task1 versions and Task2 exact load balancer ID, privately supplied valid TLS Secret, healthy authenticated Raptor deployment.
Produces: GitOps ingress release and two HTTPS host routes.
- [ ] Write failing route contracts for the two exact hosts, frontend/open-api destinations, TLS-only credential access, no management routes, unknown-host rejection and required TLS Secret reference.
- [ ] Implement namespace-scoped Kong GitOps project/release and routes. Constrain chart-required cluster resources explicitly; do not expand the existing Raptor project silently.
- [ ] Exercise frontend proxy authentication with no credentials, normal user and administrator; verify Secure cookie/CSRF/forwarded scheme behavior. Fix only demonstrated defects with failing regression tests first.
- [ ] Obtain or reuse only a certificate whose SANs match both exact names. Transfer private key only via authorized private bootstrap; never commit it.
- [ ] Run render/authentication contracts and relevant Go tests; commit. Keep routes unpublished until platform initialization and administrator setup succeed.

## Task 4: Review and live installation
Files: docs/setup/rdev-kong.md; working/design/2026-10-06-kong-public-ingress-result.json outside public source for sanitized receipt.
Consumes: reviewed commits, actual cloud IDs, private TLS Secret, actual user authentication.
Produces: verified public HTTPS access and explicit remaining limitations.
- [ ] Run all relevant tests and one fresh independent whole-branch review, fix important findings with regressions; attach every created PR and integrate only after required checks pass.
- [ ] Apply reviewed Kubernetes resources through the existing private Workbench, with concrete action-time confirmation for new controller permissions where required.
- [ ] Verify Argo Synced/Healthy and proxy/controller Ready. Check DNS and TLS hostname validation without disabling certificate checks.
- [ ] Verify public browser login, authenticated HTTP/MCP, unauthenticated denial, HTTP redirect/no plaintext credentials, and absence of public management paths.
- [ ] Record actual versions, Git revision, load balancer/DNS IDs, cost and acceptance results. Keep incomplete agent execution clearly marked; no simulated success.

## Self-review and handoff
All spec sections map to Tasks1–4. Versions and prices are evidence-gathering steps rather than invented constants. Task2 owns cloud lifecycle, ACK owns listener/backends, Task3 owns routes; private database/bootstrap dependency is unchanged. No installation or new billable resource has been performed by writing this plan.
