# PgCat complete observation implementation plan

> Use superpowers:subagent-driven-development, one sole implementer at a time and scoped independent review per task. No routine owner approval pauses; design approved.

**Goal:** Read a complete fresh PgCat observation within existing capacity/deadlines, and fail before model use if actual agent read is not ready.
**Architecture:** Additive clients branch on existing logical MCP tool, one checked private client/fixed8two-query batch, shared guarded private preflight before model/AXstartup.
**Tech:** Existing Go Infra API/Gateway, Node harness/MCP, node:test/Go fixtures.
**Spec:** docs/superpowers/specs/2026-11-01-pgcat-metrics-bundle-design.md

## Global constraints
Existing checkout only; preserve unrelated untracked/iCloud files and branch59e1afa. Source base01c48fc. No cloud/GitHub/credential actions by implementers/reviewers. Only11listedproductionfiles plus tests/docs; surface necessary expansion. Same MCP/tool/model interfaces, no capacity/IAM/resource/cache/framework/newroute, no deadline/freshness/coverage/auth/approval relaxation. All current legacy/single-series compatibility. Credentials never logged. Replacementfalse. Workstop22:30Shanghai/mutationcutoff21:30. No repeated whole reviews/test suites without a new change.

## Task 1 — additive complete resource MCP observation

Files: Infra domain/proxy_observation.go; provider/{pgcat_observation,prometheus,proxy_observation}.go; api/{server,operations,resource_operations,resource_mcp}.go; covering tests only.
- [ ] RED new real HTTP/MCP resource clients selection with agentBasic/check/fullproof fixtures; current implementation rejects clients.
- [ ] Define PgcatClientsObservation exact JSON series,observedAt,fleet,backend,samples. Fixed8samplekeys per spec; no raw re-stamping or normalization. Optional bundle interface avoids changing unrelated readers/fixtures; retain old ProxyObserver contract if practical.
- [ ] Resource-only enum/validation: accept clients only through existing logical resource handler, same exact selectors. Legacy physical clients selection rejected before provider. Old8series paths unchanged.
- [ ] Provider reuses one checked mapping/group/member inventory and existing backend health validation (factor reusable helper, not duplicate guards). One private ACKclient perbundle, same no-redirect/fixedService/http bounds. Max2fixedquerytasks with slotrefill/cancelqueued on failure; existing per-vector validation reused. All8orerror.
- [ ] Full expected proof vs fresh current proof before and after read; database/app realization changes reject, no partial output. Final check latency/expiry part of shared budget. Preserve source timestamps; outer metadata cannot make stale samples fresh.
- [ ] Bound complete MCP result including structured+escapedtext duplication/envelope overhead below262144bytes. Oversize returns bounded failure, no truncation; don't change oldsingle read behavior unnecessarily.
- [ ] Tests actual resource agent authorization/proofs, changedDB/appUID, no late export/cancel, sharedclientcount1, maxactive2/queuecancel/order, foreign/missing/duplicate/badraw data, fixedqueries/noURLs/credentials, payloadboundary and HTTPencodedlimit, old8single reads/legacy rejection.
- [ ] Run covering tests then full Infra go test/race andvet; selfreview/commit only taskfiles. Report exact RED/GREEN, files/tests and APIcontract for Task2. No sourcewide refactor.

## Task 2 — Gateway bundle adapter and guarded AX qualification preflight

Files: harness/gateway-observation-runner.mjs; Gatewayruntime/{observation_proxy,ax_live}.go; covering tests and existing resource-transport contract note.
- [ ] RED oneclientsRPC replacing2cloud+8series calls for metrics_read pgcatClients. Input other tools unchanged. Malformedbundle fails before rawvalidator, oldvalidator still rejects stale/missing/foreign/allpoolcoverage.
- [ ] Bundlememo scoped insideone runGatewayObservation call; acquirewhen firstnestedfleetread requested, adapter yields exactfleet/backend originals and8rawseries through unchanged validators. Strictallkeys/scope/selection checks; no response timestamp normalization or fallback. Same MCPsession/guardedfetch/20s deadline/outputcap. Update obsolete8waytests toonecall, invocationisolation/cancellation/failure tests; do nottouch observation-tools/pgcat-metrics production validation.
- [ ] RED fixed internal proxy preflight exactBinding+issuedaccess and sharednormalguards; missing/different/expired entry or busy/leasecancel/secret/oversized/nonJSON/late result refused. Refactor existingServeHTTPguardedexecution, keepHTTPauthentication/input checks, no newroute/registry/token.
- [ ] AXStart qualification-only preflight aftermaterialregistration+checkpointvalidation beforeModelLease/axRuns/intent/AXensure. Repeat resumedqualificationStart. Discardoutput/no trace injection, failclosed ifproxyabsent. Emit fixed sanitized started/succeeded/failed preflight category+duration for operator verification; no raw samples/credential/error strings in logs. Existingcleanup mustremoveentry/revokeaccess/release runtimeslot evenbeforeruntime registration; originalhandle preserved; don't fake checkpointverified.
- [ ] Test productionStart placement/cancellation/failure paths including worker issuedAccess revoke, zeroModelLease/AXensure, no retainedentry/runtime slot; existingnonqualification flows unchanged. Use existing PGfixture approach only if actualworker test requires; stop/remove ownedfixture afterward.
- [ ] Run focused Node/Gateway suites plus fullharness and affectedGateway Go/race/vet once. Selfreview/commit taskfiles/report. No cloudops.

## Controller integration and runtime closure
- [ ] Independent taskreviews + boundedfixes, final wholebranchgraph review with actualagentproof and startupcleanup coverage. RequiredCI beforemerge; standingownermergeauthorization.
- [ ] Build/publish testedsource; code-only additive Infra release usingexisting Serverlessowner, preserveALLconfig/concurrency and no newIAM, actualservicecompatread check. No extra billingcheckNov1.
- [ ] ImmutableGatewaydigestGitOpspin, renderedGatewayonlydelta (sourceinitrepo revision changes only if needed), independentpinreview/CI; freshidle/false beforemerge. VerifyliveimageID+runnerhash+readiness/clients2/2/AXidentity.
- [ ] Understanding72hACKexactcluster/worker/WorkbenchOSSauthorization renewonlywhenruntimeworkready; neveraskagainforunchangedscope; removeboth exactcontextfilesafterrun/failure.
- [ ] ONEread-onlyContinue4569/GPT6Luna/skills0.4. Observepreflight: iffailure, no modeluse/AXguest; diagnosewithoutblindretry. Ifsuccess, actualmodelreads/baseline/harmlessapproval/pause/resume/freshreads; finalcheckpoint/cleanup/idle/flag/clients/AXtasks checks beforePASS. Realreplacementdisabled/notassessed. OwnCodexIABonly. Recordhandoff/receipts.
