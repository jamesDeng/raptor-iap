# Infra API read review and decisions

# SDD ledger — plan: docs/superpowers/plans/2026-10-05-infra-api-read.md

Base: 24ba5a0. Native execution; five tasks pending.
Pre-flight Task 1→2: Reader/environment/target contracts agree.
Pre-flight Task 1→3: JSON deployment/status envelopes agree; consumer resolves environment explicitly.
Pre-flight Tasks 1/2→4: executable and SDK permissions feed packaging; no storage dependency.
Pre-flight Task 4→5: ingress outputs and exact resource inventory feed acceptance/cleanup.
Ruling: use an ignored manual Git worktree — task root is not a Git repository and native worktree setup previously failed there — cost if wrong: manual lifecycle cleanup.
Task 1 RED: API New and scope Load absent; subsequent scope test passed. Default cache was outside writable roots; exported existing private test environment fixed test runtime. Full race/vet passed.
Task 1: complete (commits 24ba5a0..038034f, tests: sh -c 'cd components/infra-api && go test -race ./... && go vet ./...' → ok  	raptor-iap/infra-api/internal/scope	(cached))
Task 2 RED: provider implementation absent. GREEN: provider and HTTP race suites/vet passed, including owner UID, repeated continuation token, account scope and missing ACK tests.
Task 2: Ruling: fetch a fresh 15-minute kubeconfig for every read — avoids shared cached credentials and always refreshes before expiry — cost if wrong: extra ACK metadata calls.
Task 2: Ruling: pinned STS/RDS SDKs lack context methods; use 5s connect/10s read timeouts and check context between calls — fixed bounded SDK calls preserve safety without unbounded goroutines — cost if wrong: cancellation may wait for one SDK timeout.
Task 2 follow-up: malformed query parsing silently omitted invalid fields; TestMalformedQueryRejected failed 200 then passed 400 using explicit ParseQuery.
Task 2: complete (commits 038034f..3352d85, tests: sh -c 'cd components/infra-api && go test -race ./... && go vet ./...' → ok  	raptor-iap/infra-api/internal/scope	(cached))
Task 3 RED: HTTPInfra absent. GREEN: Raptor race suite/vet, Infra API race suite/vet, 114 Python and 25 Node tests. New worktree required its pinned npm dependencies and Terraform provider initialization; restricted fixture sockets required approved escalation. Status fixture negative expectation corrected; adapter tests pass.
Task 3: complete (commits 3352d85..17fb8f4, tests: sh -c 'go -C components/raptor test -race ./... && go -C components/raptor vet ./... && go -C components/infra-api test -race ./... && go -C components/infra-api vet ./... && TERRAFORM_BIN=/Users/dengwei/Documents/Codex/2026-09-26/f/outputs/infra-ops-agent/raptor-iap/.raptor-local/bin/terraform /tmp/raptor-lifecycle-runtime/bin/python -m unittest discover -s tests -q && PATH=/tmp/raptor-lifecycle-runtime/bin:$PATH node --test tests/harness/*.test.mjs' → ℹ duration_ms 892.661792)
Task 4: Ruling: use documented fc_service_config function_version=3.0 and HttpTrigger fields instead of an opaque FC_HTTP_V3 backend model — pinned provider exposes FC3 directly with trigger URL and invocation role — cost if wrong: live gateway probe may reveal integration failure; no FC1 fallback.
Task 4 verified: static ELF x86-64 Linux artifact; Serverless Devs 3.1.10, fc3 0.1.25 schema validation passed; Terraform 1.13.3 / alicloud 1.293.0 validates and 2 mocked runs pass. No cloud apply.
Task 4: complete (commits 17fb8f4..06acea4, tests: sh -c 'go -C components/infra-api test -race ./... && go -C components/infra-api vet ./... && /Users/dengwei/Documents/Codex/2026-09-26/f/outputs/infra-ops-agent/raptor-iap/.raptor-local/bin/terraform -chdir=terraform-module/infra-api-read validate -no-color && /Users/dengwei/Documents/Codex/2026-09-26/f/outputs/infra-ops-agent/raptor-iap/.raptor-local/bin/terraform -chdir=terraform-module/infra-api-read test -no-color && /Users/dengwei/Documents/Codex/2026-09-26/f/outputs/infra-ops-agent/raptor-iap/.raptor-local/bin/terraform -chdir=infra-terraform/environments/poc-sg-infra-api validate -no-color' → Success! The configuration is valid.)
Task 5: Ruling: gate live creation on an owned HTTPS domain/certificate — official traditional gateway procedure requires it and account DNS inventory is empty; ask one missing-domain question while continuing local review — cost if wrong: live acceptance is delayed despite a possible usable external domain.
Task 5: Ruling: output raw gateway subdomain, not an invented verified HTTPS URL — no TLS binding exists — cost if wrong: operator must supply verified ingress separately.
Task 5: Ruling: explicitly select existing VPC_SHARED instance — account metadata proves availability without buying a new instance — cost if wrong: account edition/quote may differ; verify before creation.
Task 5 probe regression: application-level direct 401 could masquerade as FC IAM protection; classifier test failed, then passed by requiring a recognized FC IAM error code. Raw provider bodies remain excluded from output.
Task 5: Ruling: perform draft PR publication after the one final review, while closing the local implementation task with an explicit blocked live receipt — follows executing-plans review gate despite Task 5 combining review/publication — cost if wrong: task completion wording could imply cloud acceptance; receipt expressly denies it.
Task 5 local gate passed: all three Go race/vet suites, 114 legacy Python, 25 Node, 2 probe and 2 Terraform mock runs. Live acceptance blocked on missing HTTPS domain/certificate; zero new cloud resources.
Task 5: complete (commits 06acea4..5d6fb03, tests: sh .superpowers/sdd/2026-10-05-infra-api-read/verify.sh → Success! The configuration is valid.)
Ruling: stack the new draft PR on feat/go-platform-local while PR5 remains unmerged — avoids duplicating the existing platform implementation in this read slice diff and leaves PR5 unchanged — cost if wrong: retarget/rebase after PR5 merges.

## Single final review and native fix pass
- ESS PageSize 100 exceeds max50: fixed to50. Actual SDK transport regression first panicked, then failed on100, then passed on50.
- Function-wide concurrency cap absent: contract test failed then passed with concurrencyConfig.reservedConcurrency2.
- Actual SDK regression exposed nil runtime options: corrected ESS and ACK; real SDK local transport tests pass.
- Local built service actual STS identity and negative authentication passed; no cloud resources created.
- Final Ruling: gateway TLS/header forwarding/RAM/sandbox connectivity remain unverified; cost: deployment readiness pending live acceptance.
- Final Ruling: ACK network/RBAC remain unverified; cost: defer until Terraform foundation supplies cluster.
- Final Ruling: uncertain deployment cleanup remains procedural and locally tested; cost: no real cleanup receipt.
- Final Ruling: restart/scaling excluded as approved read-only scope; cost: this slice cannot operate workloads.
- Deferred minor items: none reported.

Final post-fix gate: all three Go race/vet suites, 114 legacy Python, 25 harness, 2 probe and 2 Terraform mock runs passed. Both Terraform configurations and pinned FC3 schema validate; static Linux package rebuilt. No second review was performed.

## Live integration follow-up

The [2026-10-05 live receipt](infra-api-read-live-receipt.json) supersedes the earlier ingress/RAM/sandbox blockers. Both local and actual sandbox HTTPS identity/authentication probes passed. ACK and mutation acceptance remain deferred.

- Gateway rejected PASSTHROUGH plus a header mapping. The contract regression failed, then passed after removing the mapping; live forwarding/authentication passed.
- FC3 direct rejection uses native `Code` and ACS3 framing. A regression failed, then passed after the probe normalized the error and sent a shaped negative signature. Unknown errors and application-level 401 still cannot pass IAM acceptance.
- Dedicated RAM deployment identity verified exact function read scope, then deployed with time-limited scoped actions. Its AccessKey, user and policy were deleted after testing.
- Newer E2B SDK creation returned 405. Inventory was reconciled as empty before using the project's pinned 2.31.0 SDK; successful sandbox/key cleanup was confirmed.
- Temporary function, APIs/group, roles/policies, domain binding and created CNAME were removed. Purchased domain/certificate and pre-existing shared gateway retained.
- The native follow-up changes only Terraform ingress configuration, the acceptance probe and documentation; no second whole-branch review or implementation delegation was performed.
