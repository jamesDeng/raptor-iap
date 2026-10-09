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
up=1 plus complete active/idle/waiting triplets for every observed pool/user and
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
