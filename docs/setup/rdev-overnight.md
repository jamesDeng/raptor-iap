# Pause rdev.ali overnight

The owner chose data-preserving overnight stop/start. `scripts/rdev/overnight.py` has explicit `sleep`, `wake`, and `status` modes. Without `--execute`, it only reads metadata and prints a preview. No deletion API, scheduling, scaling, protection changes or permission grants are implemented.

Use a **fresh private rdev.ali Terraform state file** pulled from the initialized environment with `terraform state pull`. Redirect it to a local ignored file with restrictive permissions; never commit it or paste its contents. The script verifies the caller account against the state, limits targets to the exact foundation addresses, and independently checks live region, VPC, ownership tags and pay-as-you-go configuration.

```sh
python3 scripts/rdev/overnight.py sleep --state-file /private/path/rdev.tfstate
python3 scripts/rdev/overnight.py sleep --state-file /private/path/rdev.tfstate --execute --confirm rdev.ali
python3 scripts/rdev/overnight.py status --state-file /private/path/rdev.tfstate
python3 scripts/rdev/overnight.py wake --state-file /private/path/rdev.tfstate --execute --confirm rdev.ali
```

Default CLI profile: `infra-ops-poc`; change with `--profile`. Use an existing authorized operator identity. This script does not broaden the GitHub deployment role. Required mutations are `rds:StopDBInstance`, `rds:StartDBInstance`, `ecs:StopInstance`, and `ecs:StartInstance` on the owned IDs; metadata reads cover STS, RDS tags/attributes, ECS instances, ACK node-pool detail and ESS scaling-group detail.

Sleep normally stops the ECS worker first and waits for both `Stopped` and `StoppedMode=StopCharging`, then pauses RDS and waits for `Stopped`. Wake starts RDS first, then the existing worker. Mutations run once; status reads poll for up to 15 minutes per action. On any failure or uncertain outcome, subsequent actions stop. Inspect `status` before repeating; do not assume partial sleep or wake completed. Running ECS does not prove Kubernetes Ready or application readiness.

This POC supports exactly one worker. Worker disks and instance identity are retained; no scale-to-zero deletion occurs. The script refuses missing/partial ACK state, unknown automation settings, enabled managed-node automation, enabled ACK auto scaling or active ESS health checks. It does **not** disable those settings. If that gate fails after ACK creation, inspect its real configuration before choosing how to pause workers. Avoid concurrent infrastructure apply/scaling during sleep/wake. External operators and later configuration changes can still restart or replace resources.

For the current partial foundation, live preview finds the owned running PostgreSQL instance and no workers. The script was verified in read-only preview; actual RDS sleep/wake and worker lifecycle have not been exercised. The missing NAT prerequisite is fixed, but ACK has not yet been created.

## Charges and restart limits

Stopping compute is not zero-cost teardown. Storage, backups, snapshots, NAT/EIP, load balancers and other retained services remain billable. Pause does not reset or extend the approved 72-hour budget window. Review billing and retained resources separately.

RDS PostgreSQL pause requires a pay-as-you-go primary instance using cloud disks, without read replicas or an enabled managed database proxy. The script checks the primary/cloud-disk/read-replica conditions; the RDS stop API enforces remaining service-side conditions. RDS automatically resumes after **15 days**, not indefinitely. Storage and backup charges continue. Source: [Alibaba Cloud PostgreSQL pause](https://help.aliyun.com/zh/rds/apsaradb-rds-for-postgresql/suspend-an-rds-instance-2), [StopDBInstance](https://www.alibabacloud.com/help/zh/rds/developer-reference/api-rds-2014-08-15-stopdbinstance), [StartDBInstance](https://www.alibabacloud.com/help/en/rds/developer-reference/api-rds-2014-08-15-startdbinstance).

ECS economical mode releases compute while retaining cloud disks. Restart may fail if the selected instance type has no stock. A stopped instance can still be in standard, billable mode, so the script checks the returned stop mode. Source: [ECS economical mode](https://www.alibabacloud.com/help/en/ecs/user-guide/economical-mode), [DescribeInstances](https://help.aliyun.com/en/ecs/developer-reference/api-ecs-2014-05-26-describeinstances).

Replacement guards use [ACK node-pool details](https://help.aliyun.com/en/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodepooldetail) and [ESS scaling-group details](https://www.alibabacloud.com/help/en/auto-scaling/developer-reference/api-ess-2014-08-28-describescalinggroups).
