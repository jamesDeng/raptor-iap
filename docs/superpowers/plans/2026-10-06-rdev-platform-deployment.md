# rdev platform deployment implementation plan

> For agentic workers: use superpowers:executing-plans to implement inline, followed by one fresh whole-branch review.

Goal: Argo CD reconciles the five existing Go platform services on rdev.ali with restricted PostgreSQL runtime roles.
Architecture: existing private ACK and RDS; a Helm platform chart plus monorepo Argo Application; private operator bootstrap. Gateway runtime execution remains disabled.
Tech: Go, PostgreSQL14, Kubernetes1.35, Argo CDv3.5.3 (official latest release checked October6), Helm, GHCR, GitHub Actions.
Spec: docs/superpowers/specs/2026-10-06-rdev-platform-deployment-design.md (owner approved October6).

## Constraints
- One existing worker, one existing RDS; do not enlarge or recreate either.
- One replica per platform service; separate raptor/gateway schemas and runtime users.
- No secrets or kubeconfig in Git, artifacts or printed output.
- No public API server, admin endpoint or new LoadBalancer in this slice.
- Simulation disabled; no synthetic execution presented as live acceptance.
- Permanent release configuration goes through the monorepo and Argo CD.
- Preserve current deployment-role permissions; any new IAM grant requires exact separate approval.
- Keep existing POC cost allocation; read actual resource headroom before deployment.

## Review focus
1. An image tag changes after publication: deploy by digest, reject unresolved digest.
2. Restricted RDS runtime users accidentally inherit schema-owner privileges: verify cross-schema denial and runtime role membership.
3. A migration partially fails or retries: fail release acceptance and retain evidence; do not drop schemas or replay destructive SQL.
4. Missing private cluster access: stop rather than enable public access or broaden IAM implicitly.
5. Gateway accepts a request but has no real runtime: retain queued status, report incomplete live execution; never enable simulation to make acceptance appear successful.

## Task1: Publish immutable platform images
Files: .github/workflows/platform-images.yml, scripts/rdev/check-platform-image.sh, scripts/rdev/test_platform_release.py.
Consumes: existing two Dockerfiles and offline packaging checks.
Produces: main-commit raptor/gateway GHCR image digests.
- [ ] Add failing publication contract checks: PR job cannot publish; main/manual publishing requires main revision; workflow token has only needed package-write permission; no personal token; amd64 images.
- [ ] Implement publishing separate from PR build/check, keep checks mandatory and record digests as public metadata only.
- [ ] Run publication contract tests and actionlint; preserve existing image packaging checks.
- [ ] Commit; include in whole-branch review before publishing.

## Task2: Kubernetes packaging and GitOps definitions
Files: helm-chart/raptor-platform/{Chart.yaml,values.yaml,templates/*.yaml}, infra-kubernetes/environments/rdev.ali/{application.yaml,project.yaml,values.yaml}, scripts/rdev/test_platform_release.py.
Consumes: published digests, secret names, generated app codes, service ports8870–8874.
Produces: five Deployments/Services, repository snapshot init configuration, Argo Project/Application.
- [ ] Add failing rendered-manifest checks: five exact services and ports; one replica; ClusterIP only; explicit bind addresses; no simulation; no literal credentials; distinct runtime Secrets; service-token automount false; digest references; resource requests/limits; correct environment/application labels.
- [ ] Add chart and environment release files. Cap database pools through connection URL pool_max_conns=10. Mount repository snapshot at a pinned revision for schemas/skills. EmptyDir checkpoint path must not be represented as durable live sandbox recovery.
- [ ] Restrict Argo project to this repository, cluster and namespace. Automatic sync/self-heal, initial pruning disabled.
- [ ] Run helm lint/template and manifest contracts; inspect resources fit available worker headroom.
- [ ] Commit.

## Task3: Private bootstrap and RDS initialization
Files: scripts/rdev/platform_bootstrap.py, scripts/rdev/test_platform_bootstrap.py, docs/setup/rdev-platform.md.
Consumes: fresh owned Terraform state, authenticated private operator connection, reviewed Git revision, private generated credentials.
Produces: restricted database roles, platform schemas/migrations, private Secrets, Argo bootstrap.
- [ ] Add failing bootstrap guard tests: wrong account/region/cluster/RDS rejected; preview has no mutations; absent private access fails closed; existing unknown database/role ownership rejected; secrets never emitted; retries do not rotate credentials or recreate users implicitly.
- [ ] Implement preview and explicit execution stages with exact owned IDs. Use official ACK Workbench/private access; confirm node Ready and namespaces first. If Workbench cannot support automation, prepare a specific private-access alternative for review rather than publishing Kubernetes API.
- [ ] Initialize only a new platform database on the existing RDS. Create schema-owner/migrator and distinct runtime logins, revoke inappropriate public schema privileges, configure worker-network access only. Keep administrator/migration credentials out of runtime Deployments.
- [ ] Run both migrations as bounded one-shot work. Bootstrap the initial admin privately using the existing supported command; no hard-coded password or credential-bearing manifest.
- [ ] Bootstrap pinned official Argo CDv3.5.3 non-HA; verify upstream artifact and Kubernetes compatibility. Populate Secrets privately and apply reviewed GitOps project/Application. No new infrastructure billing resources.
- [ ] Run offline tests before live stages, then record sanitized live outcomes per stage.
- [ ] Commit.

## Task4: Review, integration and live acceptance
- [ ] Run relevant Python/manifest/workflow tests and existing Go tests; verify PostgreSQL14 migration behavior separately from local PostgreSQL17 evidence.
- [ ] Request one fresh-context whole-branch review covering secret handling, GitOps scope, permission isolation and simulated/live boundaries. Fix important findings with regression tests; record any remaining limits.
- [ ] Create and attach a PR. Integrate under the user's existing go-ahead authorization only after required checks/review pass; don't claim deployment from merge alone.
- [ ] Publish images, pin actual digests and exact deployment revision, run the reviewed private bootstrap.
- [ ] Verify Argo Synced/Healthy; five Deployments Available; restricted database isolation; browser login; admin environment/catalog; authenticated HTTP/MCP; live Infra API discovery if reachable.
- [ ] Register rdev.ali and generated application objects, align workload labels through GitOps, repeat actual discovery. Report unsupported runtime actions as incomplete.
- [ ] Save sanitized receipt and keep environment running. Overnight stop/start remains deferred; maintenance settings remain unchanged.

## Completion
Deployment is complete only when actual Argo and workload health, database and login evidence exist. If private access, privileges or connectivity block a stage, save concrete prepared artifacts and request only the exact missing dependency. No fake fixture can satisfy live acceptance.
