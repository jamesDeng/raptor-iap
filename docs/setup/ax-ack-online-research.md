# AX on ACK: online research and support request

Checked 2026-10-08. No verified supported installation procedure for Google AX/Agent Substrate on managed ACK was found. This does not prove such support is unavailable.

## Primary sources

- https://github.com/google/ax — AX upstream.
- https://github.com/agent-substrate/substrate/blob/main/tools/setup-gcp/README.md — upstream requires PCR/CTB certificate APIs and provides GKE setup. Its creation-time/recreation warning is GKE-specific; do not apply it to ACK as a verified fact.
- https://docs.cloud.google.com/kubernetes-engine/ai-ml/install-overview-substrate — official GKE installation requires those APIs; does not establish ACK support.
- https://www.alibabacloud.com/help/en/ack/customize-ack-pro-control-plane-component-parameters-1693463976408 — ACK Pro allows selected control-plane tuning, but documented feature-gate list omits PCR/CTB/projection. It explicitly says the console is the most current list, so omission is not definitive rejection.
- https://help.aliyun.com/en/ack/ack-managed-and-ack-dedicated/user-guide/create-an-ack-dedicated-cluster/ — documentation says new dedicated ACK clusters are no longer supported since 2024-08-21; this is not a current straightforward alternative.

## Evidence and conclusion

New rdev.ali ACK1.36.2 live discovery lacks PCR/CTB, as recorded in ax-sandbox-deployment.json. Installing CRDs or cert-manager alone would not supply the native kubelet projected-volume implementation required by upstream. An alternative Secret-based certificate deployment would require upstream changes and verification, not a confirmed configuration-only ACK solution. Self-managed Kubernetes on ECS could permit feature flags but changes the requested managed ACK architecture and has not been deployed.

## Support ticket submitted

ID: 000GJSBSH0
URL: https://smartservice.console.aliyun.com/service/chat?id=000GJSBSH0
Time: 2026-10-08 12:21 Asia/Shanghai
Product: 容器服务 Kubernetes 版 ACK
Priority: 产品使用咨询
UI confirmed: 已分派; engineer assignment acknowledged.

The request includes the cluster ID, region, version, edition and sanitized discovery results. It asks for supported versions/editions, whitelist or backend enablement, exact API-server/runtime-config and kubelet gates, signer/RBAC/rotation support, in-place versus recreation requirements, and minimal projection acceptance checks. It requests fee and change impact disclosure before any paid upgrade. No kubeconfig, token, private key or access key was sent.
