# PgCat MVP worker logs Implementation Plan

> For agentic workers: use superpowers:subagent-driven-development task by task.

**Goal:** Replace the active collector-based acceptance path with retained app logs and execute the safe MVP.

**Architecture:** Ordinary Kubernetes streams retained on the existing ACK worker. Normal Raptor/Infra API execution and business guards unchanged; active operator procedure supersedes legacy collector admission.

**Tech Stack:** Python3.6 standard library, kubectl, existing Workbench CLI and Gateway guard.

**Spec:** docs/superpowers/specs/2026-11-04-pgcat-mvp-logs-design.md

## Global Constraints

No new cloud/IAM/services or dependencies. Preserve unrelated local files. Exact existing worker/Deployment binding; private files; bounded disk. No collector frame/assessor/native-controller requirement. Preserve business approvals/min2/max4/desired>=min/combined old clients<=1/freshness/no-replay/cleanup. Keep all remaining limits and prior owner decisions. No unsupported PASS.

## Review Focus

Pod replacement during discovery; Pod container restart; early stream exit; disk/write errors; stop signals and child-process cleanup. Test these in Task1 with fake kubectl streams, not mirror-only tests.

### Task1: Retain app logs and retire active collector procedure

- [ ] Add test-assets/traffic/app_log_retention.py and test_app_log_retention.py; failure-first lifecycle/ownership/disk tests, Python3.6 compatibility.
- [ ] Add docs/superpowers/plans/2026-11-04-pgcat-mvp-runbook.md. Mark old live runbook and traffic README as legacy/superseded and link active procedure. Update test-client README acceptance paragraph to app logs; do not rewrite legacy implementation or unrelated skills/runtime.
- [ ] Review the exact diff; fix meaningful findings; targeted tests. Commit, PR/CI, merge under standing authorization.

### Task2: Disabled restart/log retention verification

- [ ] Workbench disk/tool checks and daily cost check once; use standing72h ACK credential permission safely.
- [ ] Stage exact reviewed source and existing guard; verify Gatewayfalse/idle and bound app.
- [ ] Launch recording detached, verify current Pod logs, restart only bound app with replacementfalse, retain old/new logs, confirm old Pod deletion and2Ready; stop/reconcile streams and credentials.
- [ ] Report actual failures, gaps and durable files; no full replacement PASS from this check.

### Task3: Full MVP

- [ ] Fresh resource/provider/model/skill/approval readiness. Normal read-only qualification; correct any single evidenced blocker within scope.
- [ ] Before cutoff, guarded enable, one full2->4->2 request, exact approval/recheck/connection limits; assess app impact and convergence.
- [ ] Restorefalse, verify fleet/app, stop log streams, remove temporary access; retain logs/report. Stop on unreviewed recovery or unknown mutation.
