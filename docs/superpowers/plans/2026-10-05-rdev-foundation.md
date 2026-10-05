# rdev.ali Foundation Implementation Plan

> For execution: use superpowers:executing-plans. Preserve the user's previously selected native execution method; do not dispatch implementation agents.

**Goal:** Deploy the platform into a real Singapore ACK environment and verify authenticated use, GitOps reconciliation and live discovery.

**Architecture:** One ACK Basic cluster, one 4-vCPU/8-GB worker, managed PostgreSQL with separate Raptor/gateway schemas. Terraform/GitHub Actions owns infrastructure; Argo CD owns Kubernetes releases. Pi remains in Agent Sandbox and Infra API remains FC/API Gateway.

**Tech stack:** Go, PostgreSQL 14, Terraform, Helm, Argo CD, Prometheus, GitHub Actions, Serverless Devs.

**Spec:** ../specs/2026-10-05-rdev-foundation-design.md

## Global constraints
- Environment rdev.ali; region ap-southeast-1; one cluster and one worker for initial functional tests.
- 72-hour initial window from successful provisioning; RMB150 foundation allocation; cumulative POC below RMB1000. Allocation is not an automatic provider spending cap.
- Public monorepo; no plaintext credentials, Terraform state or private provider logs in Git.
- No automatic merge of PR5/PR6; determine current merged state before choosing the release commit.
- Do not pre-create business RDS/PgCat outcomes. Preserve existing sandbox resources, domain and certificates.
- No cloud apply until stock, permissions, complete cost worksheet and cleanup ownership pass preflight.

## Review focus
- Small worker rejects ACK admission: check selected four-core type, network plugin and zone before apply.
- RDS unavailable or connection-limited: use bounded pools; startup and readiness fail visibly without destructive migration retries.
- Partial cloud creation: retain state, reconcile inventory before retries; never recreate blindly.
- Authentication absent or secrets leaked: fail closed; private config; sanitized receipts only.
- Sandbox cannot reach services or uses simulated adapters: acceptance explicitly separates live calls from simulation.

## Task 1: Deployment preflight and budget evidence
Files: create docs/setup/rdev-foundation.md and scripts/rdev/preflight.py; update the design with observed results.
Consumes: existing infra-ops-poc profile and reviewed source commits. Produces: private preflight JSON with account/region, source SHA, stock, permission checks and itemized 72-hour costs.
- [ ] Verify PR5/PR6 state; choose an integrated source SHA without implicitly merging.
- [ ] Use an isolated worktree for this slice. Copy the approved design/plan into repository docs/superpowers/specs and plans, preserving private data exclusion.
- [ ] Query four-core worker stock and ACK supported cluster versions/plugin/node-type constraints; record eligible zone/type.
- [ ] Verify PostgreSQL pg.n2e.1c.1m with 10 GB high-performance storage, price and required service-linked role. Do not assume cloud_essd API pricing represents high-performance storage.
- [ ] Price two CLBs, NAT/EIP, traffic allowance, Prometheus disk, FC/API Gateway and existing sandbox/checkpoint charges. Fixed baseline is approximately RMB112.79/72h; full forecast must fit RMB150.
- [ ] Implement preflight(account_profile: str, region: str, source_sha: str) -> sanitized/private receipt pair; verify expired credentials and unavailable stock stop before apply. Never print credential-bearing provider errors.
- [ ] Commit the source-free preflight documentation and checks.

## Task 2: Cloud foundation Terraform
Files: create terraform-module/rdev-foundation/{main,variables,outputs,versions}.tf; tests/contract.tftest.hcl; infra-terraform/environments/rdev.ali/{main,variables,outputs,versions,backend}.tf; .github/workflows/rdev-foundation.yml.
Consumes: verified preflight configuration. Produces: cluster ID, private RDS endpoint, VPC/vSwitch identifiers and private kubeconfig reference.
- [ ] Write mocked contracts checking ACK Basic, one four-core worker, private RDS, bounded instance count, managed-resource tags and no default public database access.
- [ ] Run terraform test; require the new assertions to fail before implementing resources.
- [ ] Implement dedicated environment VPC, private worker subnet, RDS subnet, NAT/EIP, ACK and RDS. Manage prerequisites explicitly. Use existing remote-state/locking conventions where supported; state must not enter Git.
- [ ] Publish plan on PR; apply only the reviewed commit using scoped deployment permissions and serialized environment execution. Store database credentials through private provisioning inputs, not outputs/artifacts.
- [ ] Run terraform fmt -check, validate and mocked tests; inspect actual plan resource inventory and cost evidence before apply.
- [ ] Apply once, reconcile unknown outcomes, independently verify cluster/node/RDS readiness. Start 72-hour window only after successful provisioning.
- [ ] Commit sanitized provisioning evidence.

## Task 3: Platform images and GitOps release
Files: create components/raptor/Dockerfile, components/agent-gateway/Dockerfile, .github/workflows/platform-images.yml; helm-chart/raptor-platform/{Chart.yaml,values.yaml,templates/*}; infra-kubenates/environments/rdev.ali/{argocd-app.yaml,values.yaml}; scripts/rdev/bootstrap.sh.
Consumes: source SHA and Task 2 cloud outputs. Produces: immutable image digests and Argo CD release of four Raptor processes plus gateway.
- [ ] Build each existing Go entry point into minimal runtime images. Publish digest-pinned GHCR images from reviewed commits.
- [ ] Add Helm validation of five services, resource requests, secret references and database pool caps <=10 per owner; reject missing secret references and invalid image digests.
- [ ] Implement single replicas, readiness probes, private service networking and a shared HTTPS application ingress. Backend and gateway own their migrations and DB roles; frontend/admin/open-api never get database credentials.
- [ ] Bootstrap non-HA Argo CD once and point it at the environment folder. Keep admin, Argo CD and Prometheus operator access private; preserve auth and existing TLS materials. New hostname certificates must be obtained rather than reusing an unrelated certificate.
- [ ] Deploy small Prometheus with priced persistent storage and short retention, plus deployment metrics needed by existing read adapters.
- [ ] Run Helm lint/template checks, image entry-point checks and migration/schema isolation tests. Apply GitOps manifests; verify sync, readiness and one harmless desired-state correction.
- [ ] Record observed memory/CPU usage and service health, not inferred success from sync alone.

## Task 4: Real service integration and acceptance
Files: modify components/infra-api/s.yaml and environment configuration through approved deployment mechanisms; create scripts/rdev/acceptance.py and docs/setup/rdev-foundation-acceptance.md.
Consumes: deployed services, cluster metadata, existing authenticated Infra API implementation and sandbox lifecycle. Produces: sanitized live acceptance receipt.
- [ ] Redeploy Infra API with dedicated scoped identity and configure live ACK discovery; distinguish this from the earlier deleted read probe.
- [ ] Configure rdev.ali in admin; register frontend/backend/open-api/admin/gateway application objects with generated codes. Add raptor.appcode labels to their workloads through GitOps.
- [ ] Verify browser login and selected-env live deployment listing; incorrect credentials fail, absent deployments remain empty rather than fabricated.
- [ ] Verify request persistence and live progress through Raptor/gateway; independently restart services and confirm database persistence/schema isolation.
- [ ] Verify actual Agent Sandbox can access Raptor MCP and required HTTPS endpoints. Report real Pi execution separately from simulated lifecycle tests.
- [ ] Record foundation limitations: no completed RDS/PgCat agent case, no node-failure availability, no cloud mutation claim unless directly exercised.
- [ ] Commit acceptance results only after all claimed live checks pass.

## Task 5: Cost check and teardown readiness
Files: create scripts/rdev/inventory.py and docs/setup/rdev-foundation-cleanup.md.
Consumes: Terraform state and observed cloud IDs. Produces: owned-resource inventory and teardown plan.
- [ ] Reconcile current bill and unbilled estimates against RMB150 allocation and global RMB1000 budget before further cases.
- [ ] Inventory ACK-created CLBs/disks as well as Terraform resources; ensure tag ownership excludes existing sandbox/domain/OSS resources.
- [ ] Document backup and restore of platform metadata, followed by teardown of worker/disks, RDS/backup retention, CLBs, NAT/EIP and temporary identities.
- [ ] At the 72-hour boundary request continuation or teardown; do not silently delete platform data or claim that stopping Pods ends billing.

## Self-review
The tasks cover infrastructure, image/release wiring, persistence, live discovery and runtime reachability. External stock and price verification are explicit pre-apply checks, not implementation claims. Existing unmerged work is a release dependency. Business scenario skills and PgCat mutations are subsequent slices, preserving the foundation's bounded purpose.
