# rdev.ali platform deployment

Status: concrete deployment design for review; not deployed. User priority on October 6: deploy Argo CD and Raptor first, defer overnight stop/start. Builds on the previously approved rdev foundation and Go service contracts.

## Outcome and scope
Install non-HA Argo CD and deploy the four existing Go Raptor services (frontend, backend, open-api, admin) plus Go agent-gateway on the existing Singapore ACK cluster. Use one replica each and the existing private PostgreSQL 14 RDS. No new worker, database instance or public Kubernetes API. Raptor catalog creation remains separate from deployment. Register rdev.ali and these applications after migration and login succeed.

This slice establishes real platform hosting and GitOps reconciliation. The Go Gateway currently implements a simulated worker only. Deploy its HTTP/progress service with simulation disabled; do not represent queued requests as real Pi execution. Real sandbox adapter and restart commands are subsequent implementation work. Infra API stays in Function Compute, Pi stays in Agent Sandbox.

## GitOps and packaging
Use the existing public monorepo. Add reusable packaging under helm-chart/ and environment release/Application definitions under infra-kubernetes/environments/rdev.ali/. Preserve the repository spelling infra-kubernetes consistently. Kubernetes persistent configuration is committed and reviewed, then reconciled by Argo CD. Existing infra-terraform remains responsible for cloud infrastructure.

Extend the existing platform image workflow to publish main-commit images to GHCR using the workflow GITHUB_TOKEN, with no credentials in pull-request jobs. Pin deployed images by digest and official Argo CD/chart versions. Build artifacts must pass existing packaging checks. Use the public repository for Argo read access, without a GitHub credential. Check package visibility before relying on anonymous pulls.

Bootstrap Argo CD once using version-pinned official upstream manifests/chart via private operator access; subsequently retain its settings and platform Application in Git. Restrict the platform Argo project to this repository, existing cluster and platform namespace. Use automatic sync/self-heal for the selected platform path; leave automatic pruning disabled initially to protect data and accidental deletions.

## Services and secrets
Five separate Deployments and ClusterIP Services. Explicit listen addresses 0.0.0.0; cross-service URLs use Kubernetes DNS. Label each workload raptor.appcode with its generated catalog application code and the selected environment. Non-root containers, no service-account API token mounts for Raptor/Gateway, resource requests and limits, startup/readiness checks based on available service behavior. Health probes must not claim database readiness from a listener-only endpoint.

Argo CD, Raptor admin and initial Raptor browser access remain private. Use authenticated operator access/port forwarding for initial acceptance. Public HTTPS and sandbox-to-MCP reachability require a separate concrete network deployment, not an incidental public LoadBalancer. Existing basic service/browser authentication stays enabled.

Kubernetes Secrets are populated out of band; Git contains references only. Generate credentials privately, do not print them or publish values in logs/receipts. One database with separate raptor and gateway schemas, schema owner roles and restricted runtime logins. Gateway cannot read/write Raptor tables, or vice versa. Run schema migrations as a one-shot operation; migration credentials are not mounted in service Deployments. Configure pgx pool_max_conns=10 independently for backend and gateway, reserving RDS connection headroom.

Prepare a read-only repository snapshot for operation schemas and pinned skills-release lookup, with explicit revision and no embedded Git credentials. Gateway checkpoint directories are not evidence of durable live sandbox recovery; no simulated checkpoint is used as a real acceptance result.

## Access and execution order
Existing ACK API endpoint stays private. First verify Workbench access and node Ready status, namespace inventory and available resources. Official Aliyun documentation supports Workbench for internal clusters; Cloud Shell requires public connectivity. Do not add public API access or broaden deployment IAM just to simplify access. If additional access is required, prepare the exact grant and ask separately.

Then publish tested images; create only the platform database/logins on the owned RDS and allow only the owned worker network; populate Secrets; run both migrations; bootstrap administrator privately; install Argo CD; sync platform release; register environment and application records. No business RDS or PgCat is created by this slice.

## Acceptance and limits
Verify Argo Synced/Healthy at the exact Git revision; all five Deployments Available; successful PostgreSQL connections through restricted users; cross-schema access rejected; browser login and catalog/environment reads; HTTP and MCP authentication; an application deployment returned by live Infra API and matched by its real code/environment label. Record each check separately; lack of live discovery must be reported as incomplete rather than replaced with fixtures. Confirm actual ACK version compatibility and PostgreSQL 14 migrations instead of assuming the local PostgreSQL 17 tests suffice.

Record image digests, Git revision, resource IDs, checked timestamps and sanitized acceptance outcomes. Never include kubeconfig, passwords or raw secret-bearing logs. Keep cloud resource counts unchanged. Retained NAT/EIP, disks and RDS continue billing; the original below-RMB1000 POC budget and existing foundation allocation remain in force.

## Sources
- Existing foundation design: raptor-iap/.raptor-local/worktrees/rdev-foundation/docs/superpowers/specs/2026-10-05-rdev-foundation-design.md
- Existing local Go contracts and limits: raptor-iap/.raptor-local/worktrees/rdev-foundation/docs/setup/go-platform-local.md and go-platform-local-acceptance.md
- Actual image workflow: raptor-iap/.raptor-local/worktrees/rdev-foundation/.github/workflows/platform-images.yml (push:false at inspection)
- Actual service entry points: components/raptor/cmd/{backend,frontend,open-api,admin}/main.go; components/agent-gateway/cmd/gateway/main.go
- ACK read-back October6: cluster running, one healthy serving worker, no platform workloads verified yet.
- https://help.aliyun.com/en/ack/serverless-kubernetes/user-guide/use-kubectl-on-cloud-shell-to-manage-ack-clusters-1690962464408
- https://argo-cd.readthedocs.io/en/stable/getting_started/
