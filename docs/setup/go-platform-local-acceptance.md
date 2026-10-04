# Local Go platform acceptance

Date: 2026-10-04. Product source tested: `c68c6f02414729defac3e410d9d6cd2a29a9f6ce` on `feat/go-platform-local`. The platform specification is [2026-10-04-platform-service-contracts.md](../superpowers/specs/2026-10-04-platform-service-contracts.md). This receipt covers a local simulated slice, not cloud POC completion.

## Environment and results

- macOS, Go 1.27.1, PostgreSQL 17.11 in a dedicated loopback cluster on port 55432; minimum supported Go version remains 1.25.
- `go test -race ./...` and `go vet ./...` passed in both `components/raptor` and `components/agent-gateway`.
- `node --test tests/harness/go-platform-web.test.mjs`: 5 passed.
- Full offline Node harness suite: 23 passed with the prepared Python 3.12 runtime on PATH. Its cross-language test fails with the system Python 3.9; no legacy product code was changed to hide that environment mismatch.
- Full offline Python/Terraform suite: 114 passed using the prepared Python runtime and existing Terraform binary. Terraform used the existing offline fixtures; no cloud resources were applied.

Commands used from the isolated worktree:

```sh
cd components/raptor
go test -race ./...
go vet ./...
cd ../agent-gateway
go test -race ./...
go vet ./...
cd ../..
PATH=/tmp/raptor-lifecycle-runtime/bin:$PATH node --test tests/harness/*.test.mjs
TERRAFORM_BIN=/Users/dengwei/Documents/Codex/2026-09-26/f/outputs/infra-ops-agent/raptor-iap/.raptor-local/bin/terraform /tmp/raptor-lifecycle-runtime/bin/python -m unittest discover -s tests -q
```

The private test environment supplied PostgreSQL credentials and isolated Go caches. It is ignored and is not part of this receipt. Module tests create and remove only uniquely named disposable databases. Runtime SQL privileges are exercised as `raptor_app` and `gateway_app`; their administrative test connection is not a proposed production credential.

## What the acceptance scenarios prove

The Raptor acceptance test starts local backend, Open API and frontend HTTP servers and a real Gateway executable. It creates a catalog database without a deployment, submits and durably dispatches a validated request, pauses a simulated runtime, changes its pinned skills release, processes a human approval, handles signed submitted-review/merge deliveries, restores the same request and reads its saved progress. A second request exercises denial with Block. A direct restart request retains one simulated success and one rejected target independently. Execution receipts explicitly identify `simulated` evidence.

The Gateway acceptance test checkpoints a request, releases ownership at review, restores its inert conversation fixture into a fresh simulated adapter with selected skills, and completes after continuation. Progress remains ordered and persisted. Queue tests cover duplicate receipt, exclusive runtime ownership and refusal to replay unresolved ownership. Cleanup failure keeps the slot blocked. Retry counts persist across Store instances and allow two retries for an original apply run.

Approval tests bind request/action/interface/environment/target/parameters, reject parameter changes and another action ID, require denial guidance or a preset, and deny blocked requests. Official MCP SDK tests initialize, list and call tools over the actual Streamable HTTP transport with service authentication, and reject a cross-environment deployment read. No catalog mutation tool is exposed.

Restart tests use barriers to prove parallel starts, preserve successful targets across a retry, avoid re-submitting an ambiguous outcome, reject changed workload identity before mutation, and distinguish lookup/deadline uncertainty from rollout success. A common deployment status read observes generation and ready replicas; an accepted submission alone is not success.

The browser check used disposable synthetic catalog/deployment data and a fixture Gateway reader. It verified sign-in, the generated database operation form, submission, simulated progress, expandable event details and restoration of the same Request page after a full refresh. The page follows the supplied Raptor sidebar/environment/tab pattern without embedding the private reference screenshots. The fixture was stopped afterward; it is skipped in ordinary tests.

## Limits

No real Pi/model request, sandbox provisioning, OSS checkpoint restoration, ACK/Argo CD deployment, RDS creation, ECS/PgCat replacement, cloud Infra API mutation or zero-failed-operations measurement was performed by this slice. Existing sandbox/Python verification remains separate. The local simulated approval/review sequence is an acceptance fixture, not a prescribed infrastructure workflow.

The real Infra API adapter, Function Compute deployment, real agent runtime adapter and cloud acceptance remain subsequent work. These tests do not establish exactly-once external mutation, global secret redaction, live cloud SKU validity or production multi-user authorization beyond the selected simple authentication contract.
