# PgCat monitoring target discovery, rdev.ali

## Intent and current evidence

The current Prometheus chart reads `/inventory/targets.json` from the external `raptor-test/pgcat-provider-inventory` ConfigMap. Its `file_sd_configs` refreshes the mounted file every five seconds, but the chart has no inventory writer. The only current automatic write path during a replacement is the Agent-authorized `pgcat_monitoring_sync` Infra API command. Consequently, a healthy ESS node can have an exporter on port 9930 and still have no per-node series until that command runs.

## Design

The Raptor backend calls a fixed Infra API service route once per minute when `pgcatMonitoring.reconcileEnabled` is true. The route accepts only the existing service principal and an empty POST. It resolves `rdev.ali/DP00001` from the catalog on each run. The provider then requires the pinned account, region, ACK cluster, ESS group, NLB server group, target database and PgCat port, independently reads the exact ESS group and NLB server group, and publishes only a complete converged set of healthy ESS members with distinct available private IPv4 NLB addresses. The existing ConfigMap writer uses a UID and resourceVersion JSON patch and readback; concurrent updates fail closed and retry on the next tick.

During scale-out, incomplete membership does not replace the old list. At four converged members, both new nodes enter the list. A zero-weight old backend remains in the list while its ESS member is healthy: draining excludes new TCP connections, but existing sessions still need metrics. Once the old member leaves ESS, it is excluded, even if NLB still has a lingering backend. The next complete two-member observation replaces the list. Prometheus then removes its target when the ConfigMap projection and file discovery refresh. This is eventual, so acceptance checks the active target API, not only ConfigMap data.

The Agent sync command, grant route and old request compatibility remain during this stage. The Infra API's existing metrics gates require complete per-node families, `up=1`, matching labels, and fresh raw/recorded timestamps; a newly discovered target alone cannot pass a replacement gate. Removing the Agent sync step, tool and authorization is a later PR after isolated live acceptance.

## Release and acceptance

The function code and the Raptor backend are released through their reviewed workflows. The Terraform module adds the service route to the existing API Gateway group, without FC trigger management permissions. The chart's scheduler switch defaults to false and remains false in `rdev.ali`. After code and route release and after the separate POC is idle, a second reviewed GitOps PR enables the switch. The Terraform plan must be posted as a sanitized PR comment and the apply must use the protected environment. No workstation cloud mutation is part of this work.

In an isolated test, record the existing target list and all raw series. Add a healthy replacement pair without any Agent monitoring sync call. Require the scheduler to publish the exact four IDs and private addresses, Prometheus active targets to include them, and fresh complete active/idle/waiting, observation timestamp, and `up` series for each. Prove a deliberately incomplete or stale family still holds the replacement gate. Drain the two old nodes at weight zero and prove they remain scraped while existing SQL succeeds. After actual retirement and two-member convergence, require the old IDs absent from the ConfigMap and active targets; retained historical series must not satisfy the fresh instant-query gate.

The scheduler can lag up to a minute, plus Kubernetes ConfigMap projection and Prometheus discovery delay. A failed provider read or patch leaves the prior target set intact and is logged by the backend for retry. It cannot manufacture freshness or bypass the Infra API metrics gate.
