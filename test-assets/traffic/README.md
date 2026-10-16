# Retained traffic evidence

The operator collector runs on the verified ACK worker using its authorized
Kubernetes configuration. It discovers the test-client Pod UIDs every second,
reads each process's counters and event sequence directly, and fsyncs allowlisted
JSONL outside the Pods. No database credential, SQL payload or auth token is
retained. Give each run a fresh private output path. Duration is bounded to 30
minutes. Collectors never mutate cloud or Kubernetes resources.

The integration operator supplies an atomic provider-inventory JSON file with
`observedAt`, `desired`, complete ESS `members` and NLB `healthyRegistered` IDs.
Refresh it from authoritative provider APIs every 15 seconds; age over 30 seconds
makes assessment inconclusive. Prometheus coverage requires exactly those members,
`up=1` scoped with `job="pgcat",env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"`
and pool queries scoped with `env="rdev.ali",db_proxy_code="DP00001",target_db_code="D00002"`, plus complete active/idle/waiting triplets for every observed pool/user and
the expected test/poc_app identity, all with original raw source timestamps under
45 seconds. Missing Prometheus
makes the coverage inconclusive, while already fsynced traffic evidence remains.

The new client emits `traffic_final` after scheduling stops, all operation deadlines
settle and sessions close; its final HTTP evidence remains readable for 10 seconds.
The collector retains that final sample before a Pod disappears. A missing final
sample is inconclusive. Final snapshots are cached by Pod UID so shutdown of an
already finalized HTTP endpoint cannot erase its retained terminal counters. A newly discovered process requires all startup events
and no history before the capture boundary. Do not infer old counts from new zeros.
Rolling restart before deploying this client revision cannot prove the old process
boundary; begin acceptance only after the new revision has stabilized.

PASS requires successful observed traffic, covered event/counter increments,
settled boundary work, an active client roster with scheduling progress throughout
the measurement phase, complete provider/metrics coverage, ordered polls no more
than three seconds apart and an explicit completion marker. Failure, timeout or
ambiguous SQL outcomes produce FAIL. Skips, cancellation, truncation that loses
needed events, unknown lifecycles or missing observations produce INCONCLUSIVE.
A separate settling phase extends the boundary by at most five seconds; it cannot
resume measurement or conceal stopped traffic during measurement. Completion is
required explicitly, including for direct assessment calls. The ring's historical truncation flag alone is not a gap if every required
sequence since the retained boundary is present.

Run fixtures with:
`python3 -m unittest discover -s test-assets/traffic -p 'test_*.py'`.
Run `worker_capture.py --output <private-new-path> --inventory <private-path>
--seconds 60` on the worker with both Python files in the same directory. Preserve
JSONL, completion and summary together. Assessing an interrupted file requires
`completed=False`; do not fabricate its completion marker. Integration stores
bounded artifacts in private encrypted OSS with expiry; local output is bounded
by this run's duration. Source code alone does not establish live qualification.

## Bounded authoritative provider feed

Run `provider_feed.py` on the Mac only after runtime access and ConfigMap update
are authorized. This tool reads Alibaba APIs through profile `infra-ops-poc` in
`ap-southeast-1`, scoped to ESS `asg-t4necb4dfmu6o3z2rn0g`, server group
`sgp-xs5s42tihtl6ja2a67`, and VPC `vpc-t4n4fi6r1a7bi6n93ftq3`. It uses the
project-root `working/tools/workbench/workbench` to publish to worker
`i-t4nj1bitmcz7cuoch82h`. Supply absolute paths, a fresh unique worker inventory
path in a private directory, and the already authorized worker kubeconfig path:

```sh
python3 test-assets/traffic/provider_feed.py \
  --workbench /absolute/project-root/working/tools/workbench/workbench \
  --worker-inventory /root/private-run/provider-inventory.json \
  --kubeconfig /root/private-run/authorized-kubeconfig \
  --interval 15 --max-seconds 5400
```

No credential is fetched or transferred by this tool. The worker must already
have Python 3, kubectl, and authorized Kubernetes access; the kubeconfig must be a
regular mode-0600 file. The default listener `lsn-7kuvcqkaz5o3z2w376@6432` comes
from historical configuration and is validated against live listener attributes
and health on every read. Supply `--listener-id` if the reviewed binding changes.
No saved configuration or historical listener value establishes current health.

ESS pages must have stable counts and page numbers with complete distinct healthy
members in service or protected. ECS must supply one distinct private address per
member in the exact VPC. NLB enumeration follows every cursor with stable counts,
rejects duplicate/foreign backends, and reads both `GetListenerAttribute` and
`GetListenerHealthStatus`. Truncated health or mismatched listener/server-group
scope refuses publication. `healthyRegistered` excludes zero-weight and explicitly
non-normal backends; status and weight alone cannot establish health.

The worker file is atomically replaced in its own directory, fsynced and mode
0600. It contains exactly `observedAt`, `desired`, sorted `members`, and sorted
`healthyRegistered`. Private addresses exist only transiently for metrics target
reconciliation; they never enter the worker inventory or retained JSONL. The feed
reads the existing `raptor-test/pgcat-provider-inventory` targets at startup, then
patches only its `targets.json` on a verified roster/address change, using its
current resourceVersion. It never creates the ConfigMap or any cloud resource.
Targets use the existing PgCat port 9930 and DP00001/D00002 catalog labels.

The loop starts a read at most every 15 seconds and lasts at most 5,400 seconds.
Each refresh carries one absolute deadline capped by the interval and remaining
runtime. Provider and Workbench subprocess timeouts use the remaining budget;
remote kubectl calls share that deadline, and the worker refuses an inventory
replacement after it expires, including after file fsync. The Mac and worker
must have synchronized system clocks for the remote UTC deadline check.
A refresh/publication that misses its interval,
provider refusal, or Workbench failure stops the process with sanitized status;
it never renews freshness using cached inventory. The previous file remains
available and becomes stale for assessment after 30 seconds. A target update
that succeeds before a failed worker write may remain applied: stop and reconcile
before restarting. Remove run files during the authorized run cleanup.

Offline fixtures:
`python3 -m unittest discover -s test-assets/traffic -p 'test_provider_feed.py' -v`.
