# Retire the unused ACK sandbox

The owner authorized removal of `raptor-ax-sandbox` on 2026-10-25 after live
checks found zero nodes/node pools, no application workloads and no PVCs/PVs.
AX runs independently on the retained `raptor-ecs-ax` ECS/k3s node.

This root intentionally removes the old `module.sandbox` with `destroy = true`.
It must not recreate the cluster. Keep the original `ax-sandbox.ali` backend
and state history. The old cluster's deletion protection was disabled in the
preceding reviewed Terraform change.

The dedicated state owns the old ACK cluster, its API EIP and association,
the `10.70.3.0/24` worker subnet, and the temporary read-only PR plan grant.
The shared VPC, NAT, current rdev ACK cluster and ECS AX node are outside it.
Review the exact saved deletion plan before apply; then verify ACK-managed
API CLB, control-plane ENIs and cluster security group were also cleaned up.
If dedicated resources remain, import and retire them through Terraform.

PRs trigger the read-only retirement workflow and a sanitized plan comment.
The component's temporary plan policy/attachment are removed during teardown.
No raw state, credentials, provider logs or secret-bearing plan values belong
in the repository or public comments.
