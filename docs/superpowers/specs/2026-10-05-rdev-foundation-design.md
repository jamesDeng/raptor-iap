# rdev.ali foundation design

Date: 2026-10-05. Status: draft for review; no ACK or RDS created.

## Confirmed user decisions
Singapore Aliyun, one ACK cluster, managed PostgreSQL, Terraform/GitHub Actions for infrastructure and Argo CD for Kubernetes desired state. One public monorepo. Register the four Raptor services and Go gateway as application objects and deploy them to rdev.ali. Initial test window: 72 hours from successful provisioning; foundation allocation RMB150; whole POC cumulative budget below RMB1000.

## Proposed correction requiring review
Use one ecs.e-c1m2.xlarge worker (4 vCPU, 8 GB), 40 GB ESSD system disk, instead of the previously proposed two 2-vCPU workers. ACK documentation excludes fewer than four cores without a quota exception. A single worker is acceptable for initial functional testing only; it provides no node-failure availability. Keep one replica of each platform service and use non-HA Argo CD and small Prometheus retention. Measure resource requests before apply; do not silently add nodes or upgrade ACK tier.

## Runtime placement and data
ACK Basic hosts raptor-frontend, raptor-backend, raptor-open-api, raptor-admin and agent-gateway. Managed RDS PostgreSQL 14 Basic: pg.n2e.1c.1m, one core, 2 GB, 10 GB high-performance cloud storage, private VPC access, separate raptor/gateway schemas and runtime users. Its console maximum is 50 connections: cap each backend/gateway pool at 10, leaving migration/administration headroom. The RDS service-linked role prerequisite must be fulfilled. This platform database is separate from the later agent-created business RDS case.

Pi remains in Aliyun Agent Sandbox with encrypted OSS checkpoints; Infra API remains FC/API Gateway. Application registration does not imply moving these runtimes into ACK or deploying on catalog creation.

## Network and access proposal
Dedicated environment network managed by Terraform; do not replace/delete existing sandbox-storage network. Private workers and RDS; outbound NAT and EIP for image pulls and external APIs. Use flannel for the small initial node pool subject to ACK support validation. One internal API-server CLB and one shared application ingress CLB; public HTTPS ingress only for required platform endpoints. Keep admin/Argo CD/Prometheus private through authenticated operator access. Basic service auth and browser auth remain enforced. Resolve sandbox-to-MCP/gateway HTTPS connectivity as an acceptance prerequisite. No unrestricted public database or Kubernetes API.

## Cost evidence and bounded estimate
Account ECS DescribePrice quoted ecs.e-c1m2.xlarge plus 40 GB ESSD at CNY0.80438/hour. Console selected PostgreSQL configuration with 10 GB storage quoted CNY0.088/hour. These are quotes, not purchase guarantees. Worker stock was checked for the earlier two-core type only; validate the four-core type before apply.

72-hour fixed estimates: worker/disk 57.92; PostgreSQL 6.34; two CLB base instance fees at 0.147/hour each 21.17; one public CLB IP at 0.04/hour 2.88; cross-AZ NAT base at 0.30/hour 21.60; NAT EIP at 0.04/hour 2.88. Total approximately CNY112.79 before NAT processing, CLB LCU, outbound traffic, monitoring storage, FC/API Gateway and checkpoint usage. Remaining allocation approximately CNY37.21 is a contingency, not guaranteed capacity or a hard provider spending cap. Recheck account prices before provisioning; reject a plan whose expected costs cannot fit allocation. Dedicated small Prometheus disk must be priced in the implementation plan.

## Delivery and verification
Build/publish platform images; provision reviewed Terraform plan and database; bootstrap Argo CD; commit Helm/release definitions and register environment/application objects; verify GitOps sync, migrations, browser login, live deployment discovery and request progress. Verify actual sandbox reachability independently; simulated adapters must not be reported as live operation success. Foundation does not pre-create PgCat or the business database test outcomes. Current unmerged platform branches must be integrated through their PRs before releases; do not merge implicitly.

At the end of the 72-hour window, stop further tests until continuation or teardown is agreed. Stopping a Pod does not stop cloud resource billing. Maintain an inventory and explicit cleanup plan covering workers/disks, RDS/backups, CLBs, NAT/EIP and temporary roles; preserve existing domain, certificate, OSS checkpoints and unrelated resources. Back up platform data before destructive cleanup. No automatic recurring task or destructive scheduled teardown is created by this design.

## Sources
- ACK worker restrictions: https://help.aliyun.com/en/ack/ack-managed-and-ack-dedicated/user-guide/select-ecs-instances-to-create-the-master-and-worker-nodes-of-an-ack-cluster
- NAT fees: https://help.aliyun.com/zh/nat-gateway/nat-gateway-billing
- CLB fees: https://help.aliyun.com/zh/slb/classic-load-balancer/product-overview/pay-as-you-go
- EIP fees: https://help.aliyun.com/zh/eip/pay-as-you-go/
- Existing service contracts: raptor-iap/.raptor-local/worktrees/infra-api-read/docs/superpowers/specs/2026-10-04-platform-service-contracts.md
- Direct user choices: current conversation, managed PostgreSQL and three-day/RMB150 decisions on October 5. New worker count and network layout above are assistant proposals.
