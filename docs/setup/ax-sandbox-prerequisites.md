# ACK certificate compatibility gate

Execution date: 2026-10-08. Migration remains incomplete until all gates pass.

The exact Substrate revision is `ac41c06ee8929ee05159d679febab970a9e5cf4c`.
Its PCR client (`cmd/podcertcontroller/internal/podcertificate/client.go`,
`NewClient`) and CTB discovery (`internal/clustertrustbundle/client.go`,
`Discover`) independently prefer certificates.k8s.io/v1 and fall back to
v1beta1. v1alpha1 alone is insufficient for this revision.

Sources:
- https://github.com/agent-substrate/substrate/blob/ac41c06ee8929ee05159d679febab970a9e5cf4c/cmd/podcertcontroller/internal/podcertificate/client.go
- https://github.com/agent-substrate/substrate/blob/ac41c06ee8929ee05159d679febab970a9e5cf4c/internal/clustertrustbundle/client.go
- https://github.com/agent-substrate/substrate/blob/ac41c06ee8929ee05159d679febab970a9e5cf4c/manifests/ate-install/ate-controller.yaml

Existing ACK raptor-rdev (cluster ID c92787e953503492ea141a744c81498f1),
version 1.35.7-aliyun.1: a read-only Cloud Assistant invocation
`t-sgp6zdva1z3eoe8` succeeded using the worker's kubelet identity. Discovery
served only certificates.k8s.io/v1 CertificateSigningRequest, not PCR or CTB.
Both v1beta1 and v1alpha1 resource-list requests returned NotFound. No existing
cluster settings were changed. This does not decide the new target's APIs.

The Singapore version API independently returned 1.36.2-aliyun.1 as creatable.
The pinned Terraform1.293.0 managed-cluster schema has no certificate feature
API toggle. The ACK1.36 release notes do not establish PCR/CTB enablement.
Target discovery is required before adding worker capacity or installing AX.

A readiness receipt must include both resources in supported versions plus
successful approval, signing and kubelet projection checks. Missing evidence
means not ready. scripts/ax_sandbox/preflight.py enforces that decision.

## Minimal compatibility deployment costs

The initial root creates one vSwitch and one ACK Basic control plane, no worker,
new NAT, public API EIP, snapshot bucket or databases. ACK may create its managed
private API load balancer/security resources; inventory those after creation.
Account available balance readback was CNY918.38; this is not settled project
spend. Existing resources continue their own billing.

ACK Basic has no cluster management fee. China-site CLB documentation quotes
CNY0.147 per instance-hour plus CNY0.049 per LCU-hour. For a conservative
24-hour test assuming one private CLB and one LCU each hour:
24 × (0.147 + 0.049) = CNY4.704. This is a forecast, not a cap or actual usage;
retention extends costs. There is no public IP fee for a private CLB without EIP.
State-store requests/storage also remain billable. Reserve CNY10 for this
minimal compatibility stage; worker/storage pricing is deferred until ready.

Sources:
- https://www.alibabacloud.com/help/en/ack/ack-managed-and-ack-dedicated/product-overview/ack-pro-cluster-billing
- https://help.aliyun.com/zh/slb/classic-load-balancer/product-overview/pay-as-you-go

Private saved plan/state/logs are retained under the primary checkout's ignored
.raptor-local/ax-sandbox. Backend prefix ax-sandbox.ali is distinct from rdev.ali;
shared VPC is read through a data source and its owner/CIDR are checked. Terraform
owns only the dedicated subnet and cluster in this stage. Do not install from
this root without fresh saved-plan inspection and independent target discovery.

## Environment correction

The user clarified that this ACK belongs to the existing rdev.ali POC environment.
Terraform now lives under infra-terraform/environments/rdev.ali/ax-sandbox/ and
resource Environment tags are rdev.ali. A separate component state remains useful
for isolating sandbox lifecycle. Its existing backend prefix ax-sandbox.ali is a
legacy state identifier, not a new application environment; retaining it avoids
losing ownership of already-created resources. Terraform applied exactly two tag-only updates. Remote state readback verified
Environment=rdev.ali on both the ACK cluster and dedicated subnet.

## 2026-10-08 live gate result

The new rdev.ali ACK1.36.2 cluster fails the certificate gate: authenticated TLS discovery returns only CSR resources in certificates.k8s.io/v1, and HTTP404 for v1beta1/v1alpha1. Neither PodCertificateRequest nor ClusterTrustBundle is served. AX installation and sandbox cutover have not started.

The user explicitly authorized a public API. Terraform manages EIP `eip-t4nqsqavymz5vr33u1n3g` and its binding to ACK-owned CLB `lb-t4n9rk0g7sfk5ocmghllb`. The pinned provider ignores `slb_internet_enabled` during updates, so the module uses an explicit EIP association for this existing cluster. Private API TLS identity was verified while connecting through the EIP; ACK metadata still reports an empty public endpoint. Client certificate authentication applies; no source-IP ACL has been configured. EIP address and traffic billing are additional to the earlier CLB-only forecast.

[ACK control-plane parameter documentation](https://www.alibabacloud.com/help/en/ack/customize-ack-pro-control-plane-component-parameters-1693463976408) does not list the required certificate gates. This is absence of a documented self-service path, not proof that Alibaba Cloud cannot enable them. Provider confirmation/enabling of PCR, CTB and kubelet certificate projection is needed before proceeding.
