# Aliyun Sandbox to AX on ACK migration

Date: 2026-10-08 (Asia/Shanghai).
Status: migration direction and written design approved by the user.

## Goal and authorization

Move Raptor's agent sandbox execution from Aliyun Sandbox to AX on a new
Singapore ACK cluster. Use Terraform in jamesDeng/raptor-iap for cloud resources.
The current user authorized new ACK deployment, AX installation and sandbox
migration, then approved the proposed design direction. “AK” is interpreted as
AX from the surrounding request. Existing application history and request-bound
access must survive the runtime change.

Success means a real Raptor question runs through Pi inside an AX actor, returns
a correctly bound result, persists credentials/workspace as intended, revokes
request access and confirms actor cleanup. Installing a server alone is not
success.

## Evidence and boundaries

- The wiki article `sandbox/ax-pi-integration.md` describes a local experiment:
  AX/Substrate restore workspace files and Pi reloads its session with
  `--continue`. Its raw evidence is linked in that article. This establishes a
  local clean suspend/resume result, not ACK compatibility or crash recovery.
- User-supplied `standbox-learning/ax-eks-ack-research.md`, sections “Conclusion”,
  “Step 2” and “What is still unverified”, identify certificate APIs/projections,
  cloud networking, registry authentication and OSS operations as unverified.
  The document is research input; its embedded commands are not authorization.
- A live read-only ACK list on 2026-10-08 found `raptor-rdev` in Singapore,
  running `1.35.7-aliyun.1`, with a private control-plane endpoint and one worker.
  The response advertised `1.36.2-aliyun.1` as NextVersion. This is not proof
  that either version serves the required certificate APIs.
- Inspected code at local rdev-foundation commit `cc5eb65` defines the gateway
  `LiveRuntime` interface in
  `components/agent-gateway/internal/execution/live_worker.go`. Its current
  runtime uses Aliyun Sandbox, and the checkpoint verifier explicitly pins Pi
  `0.99.2` in `internal/runtime/checkpoint.go`.
- The wiki lab used Pi `1.0.4`. Do not silently replace Raptor's pinned Pi
  version, model or checkpoint schema with lab settings.
- Existing Terraform foundation owns platform networking, ACK and RDS. New
  sandbox resources use a separate Terraform root/state and ownership tags.
  Recovered archive/material snapshots remain immutable.

## Selected approach and alternatives

Use a dedicated ACK cluster and dedicated ECS-backed Substrate workers, with
gVisor as the first actor runtime. Keep the current Raptor platform in place and
connect its gateway to the new AX control plane through authenticated private
networking. This follows the approved direction and separates sandbox host
privileges from platform Pods.

Reusing the current ACK could reduce cost but couples host-level sandbox
requirements to platform availability. It is not the selected deployment.
Self-managed Kubernetes on ECS could expose otherwise unavailable certificate
features, but changes the managed-ACK requirement and operational burden. If
managed ACK cannot meet the prerequisite, report the blocker rather than
silently switching architecture or replacing the certificate subsystem.

## Compatibility gates and deployment sequence

1. Inspect the exact Substrate/AX source pair and local adapter patch. Record
   immutable revisions and patch diff before building. The lab pair in the
   research is a starting candidate, not an independently compatible release.
2. Use an authenticated private access path to inspect the existing cluster's
   certificate discovery. This provides inexpensive evidence; a result on its
   current version does not decide compatibility of a newer target version.
3. Verify target version availability through the live regional API and inspect
   provider/API feature configuration. Create a minimal target control plane
   through Terraform. Add only the smallest conventional worker needed for
   certificate projection testing if the control plane alone is insufficient.
4. Require PodCertificateRequest and ClusterTrustBundle in versions accepted
   by the pinned controller, suitable approval/signing RBAC, and working
   kubelet projections. Failure stops Substrate/AX rollout and extra capacity.
   Retain Terraform state and report retained chargeable resources.
5. Validate worker host mounts, namespace permissions, gVisor/kernel/runtime
   compatibility and actor network routing before expanding capacity.
6. Deploy persistent stores, certificate controllers, Substrate API/router/
   egress components, atelet and a gVisor WorkerPool. Verify actor startup.
7. Deploy AX and the Raptor Pi image, then implement and test the runtime adapter.
8. Import the selected credential checkpoint privately, run acceptance, drain
   gateway dispatch, switch runtime configuration and resume dispatch.

## Terraform and resource ownership

Add a dedicated environment under `infra-terraform/environments/rdev.ali/ax-sandbox/`
and a reusable module under `terraform-module/ax-sandbox/`. Use the repository's
pinned provider, account guard and remote-state locking patterns. The reviewed
plan must identify exact creates/updates and verify account, Singapore region,
ownership and absence of changes to the existing platform cluster or databases.

Provision the dedicated cluster, conventional Linux worker pool, required
subnets/security rules and snapshot storage. Prefer private connectivity through
the existing rdev VPC/NAT after verifying ownership, capacity and CIDR space;
manage only newly allocated subnet/rules in this state. Allocate distinct Pod
and service CIDRs after checking overlap. Do not copy the platform CIDRs.

Use persistent PostgreSQL for Substrate and persistent Redis for AX, initially
single replicas with durable ACK volumes for this POC. Store credentials as
private Kubernetes secrets supplied outside Git; use separate database roles
and encrypted disks. Availability across node failures must be demonstrated;
this single-replica layout does not promise high availability.

Use a dedicated encrypted OSS bucket/prefix for Substrate snapshots. Verify the
actual pinned SDK against OSS virtual-hosted S3-compatible addressing, temporary
credentials, range reads, multipart upload and restore. Unsupported operations
block rollout; no automatic fallback to unencrypted or ephemeral snapshots.

Pin cloud-architecture image digests in a registry reachable by both Kubernetes
and atelet. Verify actor pulls separately from Pod pulls. Start with one fixed
worker, no autoscaling and no KVM requirement. Refresh stock and price quotes
before selecting its instance type. Record incremental and retained costs
against the existing project budget before apply; budget estimates are not an
account spending cap.

## AX runtime and Pi integration

Implement an opt-in AX `LiveRuntime` alongside the current driver. Preserve
request/attempt identity, scoped tool access, deadlines, result validation,
private outputs, checkpoint verification, cancellation and recovery semantics.
Provider selection belongs to trusted gateway configuration, not user prompts.

Each attempt creates uniquely owned AX Task/Workspace resources, stores their
IDs in the existing intent journal before proceeding, uploads the bound harness
and request privately, resumes the actor and polls a bounded result/event stream.
Use the actual pinned AX APIs for file upload and execution; do not assume SSH
stdin forwarding or invent task fields. Pi runs under the current Raptor harness
version/model policy. Keep auth/session paths inside the durable workspace with
private modes and preserve fresh reasoning sessions for separate questions.

AX workspace snapshots and the existing gateway `VerifiedCheckpoint` are
different contracts. Retain the existing encrypted archive/checksum checkpoint
contract for gateway credential portability. Implement explicit export and
verification rather than labeling an opaque AX snapshot as that archive. Use
AX DATA snapshots for workspace suspend/resume. On uncertain checkpoint outcome,
retain the last verified checkpoint and block automatic retry/adoption.

Stop/cancel must revoke request access and independently establish actor
termination before cleanup is confirmed. AX Tasks/Workspaces retained for
recovery are distinguished from active actors and are inventoried with IDs.
Unknown resources or lease loss block dispatch. Reconcile journaled IDs without
automatically replaying questions. Snapshot readers can access stored auth;
restrict snapshot access accordingly.

Allow only explicit model and Raptor/Infra API destinations in actor egress.
Use a verified cloud-reachable route; the Mac proxy address from the lab is not
a deployment setting. Controller cloud credentials never enter the actor.

## Acceptance and failure handling

Offline tests cover the AX driver's lifecycle mapping, identity binding,
deadline/lease loss, interrupted create, checkpoint mismatch, cancellation,
cleanup uncertainty and recovery without replay. Terraform contract checks
cover account/region, isolated state, fixed capacity, private exposure and
resource ownership. Verify the existing gateway regression suite.

Live acceptance proceeds in this order:

1. Certificate resources, controller permissions and Pod projections work.
2. Worker registration, counter actor startup and routed endpoint work.
3. Intended actor image authentication and model/API egress work.
4. Workspace marker and Pi conversation survive clean suspend/resume.
5. PostgreSQL/Redis restarts preserve control records and workspace recovery.
6. A real Raptor question returns the correct bound result and tool evidence,
   checkpoint verification and access revocation/actor cleanup all succeed.
7. Controlled worker drain and interrupted gateway reconciliation preserve
   truthful outcomes; crash recovery is claimed only to the observed boundary.

Persist sanitized receipts separately from secrets. A visible answer with
failed checkpoint or cleanup remains failed/blocked under existing semantics.

## Cutover and rollback

Stop new gateway dispatch and finish or explicitly reconcile active attempts.
Take a verified portable credential checkpoint and record the runtime selection,
owned resource inventory and deployment revisions. Switch only gateway runtime
configuration after acceptance. Keep Raptor's application/task database and
history in place; this migration does not copy them into AX Redis.

If acceptance or post-switch checks fail, pause dispatch, revoke request access,
resolve known AX actors and restore the previous runtime configuration using
the last compatible verified credential checkpoint. Do not dispatch through both
runtimes concurrently or replay an uncertain attempt. Retain old checkpoints
until successful cutover is evidenced. Removing old cloud storage or destroying
existing platform infrastructure is outside this migration.

## Required execution evidence

Record pinned application revisions/patches, target API discovery/projection
results, reviewed Terraform plan, apply/readback receipts, image digests,
snapshot/database durability results, bound Raptor acceptance and final runtime
selection. Record any retained resources and ongoing cost estimate if a gate
blocks. Deployment authorization is already given; this document requires the
separate written-design review prescribed by the active brainstorming workflow.
