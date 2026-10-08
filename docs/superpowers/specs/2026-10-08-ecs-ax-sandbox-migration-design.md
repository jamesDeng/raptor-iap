# AX on one ECS: approved migration amendment

Current user explicitly switches the earlier ACK design to self-managed Kubernetes1.37 on one ECS in rdev.ali. This supersedes only the cluster provisioning approach; the gateway lifecycle, pinned Pi0.99.2, credential boundary, portable checkpoints, staged acceptance and rollback requirements of the earlier design remain binding.

Use one Singapore zoneA x86_64 Ubuntu24.04 ECS, initially ecs.g7.xlarge (4vCPU/16GiB), encrypted120GiB ESSD, K3s v1.37.0+k3s1. One server runs both control plane and workloads, with flannel, local-path persistence and no HA. gVisor systrap needs no KVM. Pin and verify official release checksums before running binaries. Dedicated security group and SSH key; public SSH restricted to local administrator IPv4/32, Kubernetes API and AX accessible through private VPC or authenticated SSH tunnel. No public anonymous AX/registry/database endpoints.

Reuse verified rdev.ali worker subnet10.70.1/24 and outbound infrastructure. Separate ecs-ax component Terraform state; retain earlier ACK state and resources pending explicit cleanup. Do not change existing platform resources. Provision via raptor-iap.

PostgreSQL, AX Redis and snapshots must persist across restart on encrypted disk; validate OSS compatibility before selecting remote snapshot backend. Single-node storage survives reboot, not loss of the ECS/disk; no HA claims. Existing source revision pairing is not yet build compatible and must be fixed or replaced with a tested immutable pair before installation.

Gates: native PCR/CTB discovery and projection; Substrate signer/bootstrap; counter actor networking; AX workspace lifecycle and Pi session continuation; gateway request-bound acceptance/checkpoint/access cleanup; only then cut over. Retain existing sandbox until these pass.

## Private runtime integration decision

The existing platform ACK and new K3s have different pod/service networks. Keep all cloud/OSS controller credentials in the existing gateway. Run a small runtime bridge on the new ECS: mutual TLS on private8443, then typed AX/Substrate/guest gRPC inside the new cluster. AX, guest APIs, Redis and registry receive no anonymous public exposure. Gateway uploads only verified portable Pi checkpoint bytes and request-bound MCP access to actor-private paths; the bridge receives no Aliyun control credential.

Use deterministic attempt-owned task names and a fixed Pi image/atespace. Tasks have no declarative command, so actor DATA resume does not replay an inference. Gateway journals every create, file staging and command start before mutation; process markers resolve ambiguous starts. Recovery reads exact IDs, exports already-complete checkpoint files if valid, then deletes and verifies actor/task absence; it never starts or replays a question. MCP access files are outside the durable workspace and are revoked by the existing worker. Portable archives return to gateway for encrypted OSS publication and checksum verification; AX opaque snapshots cannot satisfy that contract. Implement and test this before any dispatch cutover.
