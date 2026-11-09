# PgCat Prometheus Agent receiver

## Decision

Run a pinned Prometheus Agent container from the PgCat ESS launch template's user-data script. It scrapes the local PgCat exporter at 127.0.0.1:9930 every five seconds and remote-writes to the existing ACK Prometheus service. The receiver has a Terraform-owned intranet NLB, a VPC-only PrivateZone CNAME, and a Kubernetes NodePort. New NLB DNS names require a customer CNAME for access. The NLB and worker ingress are restricted to the private PgCat and NLB subnet CIDRs; a write-only sidecar forwards only POST /api/v1/write. Agent WAL is retained on the ECS disk for up to two hours.

The launch-template user data contains no credentials. IMDSv2 provides the ECS instance ID, which is attached as the stable series identity. The agent, receiver, and NLB are opt-in and disabled by default. Current node addresses and PgCat bootstrap inputs are unchanged when disabled.

## Ownership and prerequisites

- `terraform-module/pgcat-ess` owns the ESS launch template and its runtime IAM. The Agent needs no new cloud IAM.
- `terraform-module/infra-test-metrics-receiver` owns the private NLB, its client security group, listener, server group, worker attachment, narrowly scoped worker NodePort ingress, and the VPC-only PrivateZone alias. The NLB service-linked role is a shared prerequisite already required by PgCat.
- The rdev test foundation root composes both modules. The shared GitOps role in `terraform-module/test-gitops-access` needs the added NLB and ECS permissions applied through its own reviewed Terraform state before receiver activation.
- The ACK worker ECS ID and worker security group ID must be verified from the current cluster and supplied through protected stack inputs. The receiver server attachment must be updated in Terraform if the worker changes. An internal NLB has recurring cloud cost.
- The Helm chart owns the NodePort service and write proxy. Its `receiver.enabled` switch remains false until the service is deployed and checked.

## Staged activation

1. Merge only after the PR's unit tests, Prometheus rule tests, Terraform tests, and sanitized Terraform plan comment pass. Review the plan for any unexpected ESS replacement or live resource change. Apply the GitOps role additions through its own reviewed state.
2. Verify the current ACK worker ECS and security group IDs, then deploy the Helm receiver with `receiver.enabled=true`. Confirm the NodePort is healthy and that only POST /api/v1/write is forwarded.
3. Enable `metrics_receiver_enabled` in protected foundation inputs; review and apply its private NLB plan. Confirm its address resolves privately and that traffic from the PgCat subnet reaches the receiver.
4. Enable `monitoring_agent_enabled` only after the receiver works. Review the launch-template plan; existing ESS nodes will not gain the Agent until replaced. Verify each replacement node exports the expected identity, successful Agent remote write, complete client metric families, and fresh Infra API observations before using the metrics as a drain gate.
5. Keep the existing central PgCat scrape and inventory sync during transition. Recording rules and Infra API prefer Agent series per node when Agent `up` exists, so incomplete Agent data fails the gate instead of being filled by central scrape. Remove central scrape and sync only in a later reviewed change after live proof.

Rollback is to disable the Agent in the launch template for future nodes and continue the central scrape. Disable the receiver only after Agent writers are gone, with a reviewed Terraform plan. Do not release old PgCat nodes as a monitoring rollback.

## Limits

This receiver accepts unauthenticated remote write from hosts allowed by the private network policy. Its proxy caps request bodies at 1 MiB and does not expose the Prometheus query API through the NLB. Private reachability is the current POC boundary; stronger sender authentication is a separate production requirement. Prometheus Agent WAL buffering is bounded and cannot prove zero loss through a prolonged receiver outage. The NLB health check establishes TCP reachability, not end-to-end metric ingestion. The original central scrape remains the fallback until an independent live acceptance check.
