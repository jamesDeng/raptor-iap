# Dedicated traffic client chart

Requires generated catalog app/environment codes, immutable published image digest and an existing private DSN Secret (key `dsn`, optional CA file paths within the same mount). No secret values are emitted or created by Helm. DSN selects the dedicated proxy/database, verified TLS is the client default. For the owner-approved private POC, set `allowPlaintextPOC=true` and use a Secret DSN with `sslmode=disable`; cloud evidence remains cloud evidence. Two replicas roll with zero maxUnavailable and one surge replica; in-flight operation deadline is five seconds and termination grace twenty seconds. Extra surge traffic and reconnection failures must be included in acceptance.

No Argo Application, cluster permission or shared values were modified. Central owner installs through GitOps after image, resource capacity, private secret and app/proxy associations are reviewed. Service is private but this chart alone is not a network firewall; central network policies must restrict access. Scrape every replica directly for evidence, not a load-balanced Service endpoint that can hide a replica.


The Pod supplies `TEST_POD_UID` from `metadata.uid` and a memory-backed writable
`emptyDir` at `/run/traffic-control`, owned through the existing UID/GID/fsGroup
65532. The client creates `/run/traffic-control/finish.sock` with mode 0600. The
same distroless `/client` binary supports `finish --expected-pod-uid UID
--expected-process-id PID`; no shell, new port, service-account token or RBAC is
needed. Use the reviewed worker `finish_clients.py` boundary handshake rather
than issuing independent exec commands. See the [operator runbook](../../docs/superpowers/plans/2026-10-16-pgcat-two-node-live-test-runbook.md).

Controlled finish drains work, closes sessions, makes `/ready` false and keeps
`/health`, `/evidence` and metrics available for two minutes before exit. The
Deployment still desires two replicas; Kubernetes restores new processes. Verify
two new Ready identities and healthy SQL/metrics after capture closes. The hold
never extends the collector's five-second settling bound. Deployment and finish
require their separate live authorization gates; local tests do not establish a
verified disabled-mode path proof or replacement acceptance.
