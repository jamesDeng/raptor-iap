# PgCat causal scrape evidence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove retained retiring-node observations follow the observed app restart without comparing AX guest event time to worker/Prometheus time.

**Architecture:** Extend only the private native proof path. A fixed source-pinned Prometheus reader obtains a post-rollout TSDB watermark and three complete finite scrape samples per retiring node; sum each node's maximum complete connection total across the three. Bind this to the exact released wait and approval; the existing sealed-original assessment verifies causal order. The disabled proof exercises the same mechanism without a connection threshold on the unchanged fleet.

**Tech Stack:** Python stdlib, workerPython3.6, existing Prometheus3.15.0, existing source-verified native controller and GitOps release. No new dependency.

**Spec:** docs/superpowers/specs/2026-11-02-pgcat-causal-scrape-design.md (owner approved the linked identical proposal Nov2).

## Global Constraints

- Existing checkout only; no new project/clone/worktree. BudgetCNY3000 net, one billing read/day; today already checked. No worker/cloud/PR operation by implementation or review agents.
- Exact target rdev.ali/DP00001/D00002; existing rdev-test-metrics-metrics in raptor-test only. No public API/MCP/agent behavior changes or new resources/IAM/controller.
- Three distinct complete finite samples per retiring target strictly after post-rollout TSDB watermark. Sum the per-node MAXIMUM total(active+idle+waiting) across all three samples over BOTH retiring nodes; require<=1. Never infer causal recency from numeric timestamp order.
- Same exact source Pod/container/image/process/config incarnation; single admitted PgCat pool and one loop hash for each accepted full target/address/series identity; honor_timestamps=false, scrape-on-shutdown=false, no other writer of accepted families. Reject source replacement/config reload/foreign writers/partial or duplicate series/history replay/stale/nonfinite values.
- Fixed read-only transport, private IPv4 Pod origin, no redirects/proxies/credentials in URLs; bounded response and existing worker deadlines. Operator kubeconfig remains on ACK worker, never AX. No raw config/credentials in receipts/logs/errors.
- Use only original sealed evidence and actual process-private read issuance; caller dictionaries/summary booleans cannot grant runtime proof. Preserve sample age<=15s, provider age<30s, capture gap<=3s, existing four-/five-second closure limits and SQL FAIL precedence.
- Preserve min2/max4, desired>=minimum, exact decrease approval, resource-code Infra MCP and all withdrawal-before-restart and approval-before-reduction guards. No native enablement before matching real disabled proof including120s hold/restoration/both credential absence/global audit.
- Private native-replacement and disabled-proof plans/templates admit captureMaxSeconds only from75 through900, before preparation effects. The source-owned provider uses interval15, publishes once per tick strictly before its original runtime end, and each ingress consumes a publication once. Therefore a full900-second capture has at most60 ingress originals (ticks0 through885), within the unchanged100-original Journal-linked history bound. Do not restart/renew the provider or bypass publication ownership to extend this envelope. Generic legacy capture contracts remain unchanged; source changes require a new27-source freeze and relevant exact-worker calibration before runtime qualification.
- Owner-approved Nov3 cumulative production cap15 plus tests/docs: existing11 timing/closure paths,3 existing release pins, and only pgcat_collector_control.py as path15 for Task5. No collector/frame/provider/feed rewrite or Go Gateway guard change.

## Review Focus

- Backward clock: all selected samples must qualify; never choose the numerically latest as the post-rollout observation.
- Discovery changes: pinned source and apparent before/after equality alone cannot prove a single writer; enforce the admitted exact loop/target bound from the pinned implementation/config.
- Offline forgery: a fabricated source/sample dictionary cannot be sealed as an actual live read receipt by production collection.
- Old qualification: a pre-fix disabled proof or mismatched assets cannot open native admission.
- Unavailable evidence: pending sampling may be retried within the same wait without changing capture/feed freshness or adding an unbounded sleep; hard mismatch/SQL failure must remain fail-closed.

## File Structure

Task1: create test-assets/traffic/pgcat_causal_scrape.py (pure schema/policy/sample validation and fixed reader kernel); modify pgcat_native_controller.py only for fixed metrics-source Kubernetes/HTTP transport. Tests:test_pgcat_causal_scrape.py, focused test_pgcat_native_controller.py additions. Existing image/config/revision allowlist must be tied to independently reviewed config, never caller authority.

Task2: modify pgcat_native_ingress.py (collect/seal/validate witness), pgcat_native_assessment.py (causal acceptance), pgcat_native_closure.py (assets/admission/real diagnostic qualification), pgcat_source_bootstrap.py, pgcat_lane_actions.py, pgcat_lane_supervisor.py (new source registration), pgcat_lane_protocol.py (admission calls if needed). Source-context projection in pgcat_native_evidence.py need not change merely to add a controller-only module. Additional wrappers may change only if an actual boundary requires it, never for cosmetic consistency. Test the changed integration boundaries and update only necessary fixtures.

---

### Task 1: Fixed causal scrape kernel and source transport

**Files:** Create pgcat_causal_scrape.py and test_pgcat_causal_scrape.py under test-assets/traffic; modify pgcat_native_controller.py fixed reader methods and its test.

**Interfaces:**
- `validate_source(source: dict, policy: dict) -> dict`: exact bounded source identity/provenance including PodUID/containerID/image/build revision/process start/config hash; validates known fixed scrape topology/config/flags and accepted target identity uniqueness. Policy is source-bound release material, never a caller override accepted by itself.
- `validate_causal_samples(watermark_ms: int, matrix: dict, *, nodes: list, metric_groups: list, now: float, max_combined: Optional[int]) -> dict`: exact finite complete raw timestamp/value sample tuples; returns three samples per node and conservative combinedMaximum. For native max_combined=1; diagnostic None. Require up=1 and all baseline/discovered pool-user triplets at the same per-node timestamp, full exact scope/target labels and no duplicates; each sample>watermark and age<=15s. Never count interpolated instant/range-query evaluation timestamps as scrape observations.
- `collect_causal_scrapes(*, source_read, prometheus_get, nodes, metric_groups, max_combined, now, deadline) -> dict`: process-local issued result from actual ordered source identity→TSDBwatermark→bounded matrix reads→source recheck. No caller-supplied watermark/response argument to production issuance. Caller invokes it only after actual observed rollout; Task2 seals its actual provenance with exact frame/wait. At most the existing30s action deadline; no long blocking polling. A pending result must not claim proof or mint a successful receipt. Source matching includes original node addresses from baseline/verified inventory.
- Extend FixedWorkerReads with fixed metrics deployment/pod/config/target reads and bounded direct Prometheus GET where required; use exact name/namespace and fixed endpoint allowlist (runtimeinfo/buildinfo/config/flags/targets/tsdb/query), only prescribed selector. Keep raw responses memory-only and sanitize errors.
- If interfaces need a minor adjustment after source inspection, record exact signatures in report and preserve these responsibilities; no new generic HTTP/Kubernetes selector API.

- [ ] Write tests: missing source identity/config hash mismatch/reload or Pod/container replacement; arbitrary origin/path/redirect/proxy; duplicate loop/foreign target or writer; two samples rejected/three accepted; backward-clock numerical-last-only low count rejected; pre-watermark history rejected; per-node maxima1+1 rejected and1+0 accepted; partial/duplicate/nonfinite family/up!=1/missing baseline or discovered group rejected; timestamps paired with values; age15 accepted/>15 rejected; deadline exhaustion and sanitized errors; diagnostic permits connections>1 but retains completeness/provenance.
- [ ] Run new tests RED with supported bundledPython before implementation; record meaningful failures.
- [ ] Implement minimal stdlib kernel and fixed transport. An exact reviewed loaded-config fingerprint bound to image/revision may implement the fixed policy; a fingerprint merely captured without checking its config cannot grant admission. No YAML dependency or loose semantic YAML parser.
- [ ] Run focused kernel/controller tests GREEN, Python3.6 grammar/import checks and diff checks; inspect no read boundary widened for mutations.
- [ ] Commit only this task's source/tests; report exact interfaces, source-policy proof/counterexamples and test evidence. Source remains unable to enable native execution until Task2.

### Task 2: Bind live witnesses, original assessment and admission

**Files:** Modify native ingress/assessment/closure and source registry/admission files named above; focused tests and fixtures under test-assets/traffic. No production changes outside the14-path cap.

**Interfaces:** Consume Task1's reported actual interfaces. The existing `read_ingress` and `validate_retirement_witness` retain their outer responsibilities and return shape; extend the private witness version/schema to retain causal receipt and exact rollout-frame association. Disabled ingress must retain its equivalent actual causal read and final assessment must require it. `runtime_admission` may admit supported source capability only with the immutable assets and existing matching real diagnostic qualification path; it must never replace the hardcoded rejection with unconditional success.

- [ ] Write regression tests RED: old unqualified receipt cannot PASS; a post-rollout actual read inside the same released waiting_approval brackets binds exact request/attempt/approval/action/generation/successor identities; changed wait or source fails; no receipt before observed completed rollout; pending reads preserve published evidence age; combinedMaximum>1 prevents native witness; final assessment needs the original causal receipt and later exact approved action from a different resumed attempt, and no longer uses guest timestamps for this specific order. Preserve ordinary SQL FAIL and full discovered-group coverage.
- [ ] Add diagnostic parity tests: actual restart and successor evidence precedes the source watermark, complete causal samples required while threshold is omitted; exact-source/runtime-assets old proof invalid; complete retained close/hold/restoration/absence/audit still required for enablement. Tampered/forged/replayed receipt/source fails offline qualification.
- [ ] Integrate process-private issuance into existing ingress collection/seal; bind original frame, source, all samples, exact released wait and approval. Preserve actual production adapters and do not accept an injected fixture on the production path. Avoid waiting for scrape cycles while holding a stale capture ref; if proof collection exceeds the ingress/capture bound, retain an original bounded seed/read session and poll using subsequent ingress with the same exact wait, keeping causal ordering/source authority. No new native action unless an existing boundary cannot support it and it fits the approved scope.
- [ ] Replace cross-machine event-time order in assessment with source-verified causal witness relation; preserve sequence/attempt linkage and other freshness checks. Register/hash the new module in source-owned execution and preserve legacy/no-scale contexts.
- [ ] Run focused tests then full Python graph normal and optimized using bundledPython; actual harness graph only if modified source affects it. Validate Python3.6 syntax/import, frozen source hashes and gitdiff checks. Record duration, no duplicated frozen tests without reason.
- [ ] Commit and report exact source inventory, remaining runtime steps and all limitations. Independent task review before Task3; ONE whole-branch review after all three source tasks before root PR/release.

### Task 3: Finish existing native Gateway support and owned manual run path

**Basis:** Existing approved docs/superpowers/plans/2026-10-29-pgcat-native-closure.md Task4/C1 production wiring, not a new architecture. Owner requested completion and fullPOC; reuse approved workercontrol, no newservice/IAM/API/controller. Execute after Task2 review. Design/interface detail: .superpowers/sdd/2026-11-02-pgcat-causal-scrape/native-gateway-preparation.md (including final detached/pre-Create refinement); this draft plus GlobalConstraints and originalapprovedplan are binding.

**Files:** Existing controller, closure, protocol, actions, bootstrap, supervisor, ingress source-keyset and existing worker_control.py. No causal validator/collector/provider rewrite. Current timing union9 production paths; worker_control becomes10. Reserve3existing release-pin paths forroot after source review; no silent>14 paths. Tests scoped to actual boundaries.

**Interfaces:** Add process-owned `NativeController.bind_qualification(proof_root)`, `enable_gateway()`, `disable_gateway()`, `poll_gateway()`, `begin_create()`; no callerbool/command/receipt/URL inputs. Existing `claim_created_request()` binds one actual UIreturnedID after DISPATCHING. Existing prepare/start_capture/ingest/closure publicmethods remain native execution path, driven byroot normalRaptorUI, not a new autonomousorchestrator. CLIrun-native explicitlyrefuses unattendedentry, never falls through todiagnostic/noop.

Journal exact reserved action claim/start/complete/read methods from preparation; persist before possible sideeffect, anchor existingstrictsupport wrapper/native_support. No expiredordinarylaneaction laundering. True only with same-source/assets actual retained diagnostic closedproof includingcausalreceipt/hold/restoration/bothcredentialabsence/globalaudit, fresh ready/current capture and actual source-owned controller/Journal/reservation/supervisor/feed identity checks. Human external-producer pause is separatelylabeled operationalprecondition; verify machine-owned producers and actualGatewayidle, neverinventglobalproof fromTrueboolean.

Asynchronous flagjob uses EXISTING detachedWorkerControl, source-registered native-only, unchangedlegacymode. Bounded<=4s actualsource-owned remotePopen launch records originalworker boot/PID/start/argv/source+claim and nonmutatingready marker; rootanchorsoriginalclaim/launchinJournal before one-useexacttrigger. No longWBsession overlappedwithproviderfeed. Rootpolls onlythisoriginalownedtuple within4s; sameworkerjobretainsgenuine set/rollout/proof `_seal_action` issuance. StatusJSON/dict/callerPID cannot grant support or restart a job. Absolute workerjob ceiling151s = precheck4+set4+rolloutrequest4+rolloutstatus135+finalproof4, fixed beforestart, no renewalonpolls; ordinarylane5s and retainedproof4s unchanged. Actualrolloutstatus keepsCLI120s timeout. If any proofgroup cannotfit4s, refuse/review; don'twidenit.

Source-owned Create reservation: RESERVED whenenableclaimed, closedtoCreate untiltrueVERIFIED andfreshowner/ready/sourcechecks. `begin_create()` sealsDISPATCHING BEFORE rootbrowserclick; actualhandOffIDonce →BOUND. AbortbeforeCreate mayresetfalse onlyviaexplicit separate RESERVED-undispatched settlement withactualsameowner/unconsumedreservation/nootherjob+Gatewayidle; nofake terminalrequest. OnceDISPATCHING withoutreturnedID staysUNKNOWN. AfterBOUND, disable/restore must useactualJournalrequestID andauthenticatedterminal/confirmedcleanup/no-recovery/emptycontrol/twoconsistentreads+idle. Alreadyfalse inspects withoutmanufacturedtoggle. Unknownjob/effect neverreplays orreversefleet/killsRaptor; preserveclaims andknownoriginalowned-process boundaries, actualfalseorUNKNOWN, bothcredentialcleanup attempts, FAILprecedence, unresolvedreservation.

- [ ] RED: diagnostic/stale/tampered summary/path/source/assets cannotenable; legitimatefreshgeneration/RS/process/GatewayPodRV underimmutableassets qualifies. Missing currentownership, reservation, helperidentity, stalecapture orbusyGateway causeszeroset.
- [ ] RED: real disposable child/oneusefiles prove bounded detachedlaunch→anchor→trigger→poll while simulatedrollout exceeds5s; unknown handshake/set/rollout/result no redispatch; forgedstatus, reusedPID, claim/source/credential/inodeswap denied; genuine support requires actualsameprocessissuance. Currenthelperlegacy tests preserved.
- [ ] RED: exactrealrequest settlement/restore, running/paused/control/cleanup-unknown zero-set; alreadyfalse inspectonly; RESERVEDabort cancleantrue withno fabricatedrequest; DISPATCHINGlosthandoffUNKNOWN andneverreverse/cancel. Source-ownedCreatecannotbeginbeforetrue/consume twice. Nativeabortknown-ownedprocessesonly/bothcredentialattempts/FAILprecedence/auditrefusespendingjobs.
- [ ] Implement narrow fixedworkercontrol/nativeguard/Journal/controller bridges; source-native helperkeyregistration and fullrealdiagnostic-gatednativecapability. No localESS/NLBmutator enterscatalog. Use existingROOTsourcebootstrap/strictprivateartifacts/gatebinarypins.
- [ ] FocusedRED/GREEN then fullnormal+optimized Python, appropriate helper/branch sourcecompat/import checks; exactworker/liveproof rootlater, no cloud here. Commit/reportexactpathunion/owned APIs/jobstate/remaininglimits; independenttaskreviewthenONEwholebranchreview beforemerge/release.

## Runtime continuation (root only)

After source review/CI/merge: preserve old skills0.4 tag; coordinate new resource skill tag and reviewed existingAX/client immutable pins throughGitOps; inspect deployed source agreement. Independently review any remaining runtime admission failure before spending on a known-inconclusive run. Use WorkbenchCLI first; standing72h exact ACKworker credential transfer authorization, files0600, reuse during this run, delete both on finish/failure. Validate source policy against actual loaded config using the reviewed chart and pinned Prometheus revision, retain only sanitized hash/projection.

Run planned-volume exact-workerPython3.6 timing, actual disabled proof (one restart/finish/120s hold/restoration/credential absence/audit), then fresh forecast and full2→4→2 preflight. Fresh source/flag/quiet producers/fleet(min2/max4/protected pair)/NLB/model/skills/current request/approvals before enablement. Start retained capture before Create; native guard stays false until all gates clear. Approve only exact request-bound reduction after witness. Always restorefalse, assess original traffic, verify full cleanup; no topology-onlyPASS. Cutoff21:30, stop22:30Shanghai; do not start a run that cannot close in time.

### Task 4: Bound repeated pure timestamp parsing

**Basis:** Exact-worker Python3.6.8 valid synthetic901-frame closure exceeded existing4s/5s limits: finish6.487s and assessment6.402s. Profiling attributed over half the work to repeated pure timestamp parsing. A private memory-only4096-entry cache experiment reduced these to2.634s and2.961s, with633560 hits/901 misses. This is synthetic calibration, not qualification. Keep all limits and evidence unchanged.

**Files:** Only test-assets/traffic/capture_window.py and test_capture_window.py. Production union11 plus3release pins remains14. No new dependency, source registry key, cloud/API/IAM/controller, proof/metrics cache or clock caching.

**Exact production edit after RED:** Add `from functools import lru_cache`. Leave existing numeric branch in `seconds` unchanged; move its existing remaining string parser body verbatim into `_parse_timestamp(value)`. Replace the string branch with `return _cached_timestamp(value) if type(value) is str and len(value)<=64 else _parse_timestamp(value)`. Add `_cached_timestamp = lru_cache(maxsize=4096)(_parse_timestamp)` after the helper definition. All regex/strptime patterns, normalization, error classes and timezone acceptance remain unchanged. Cache only successful immutable built-in string-to-float results; numeric inputs, subclasses, strings longer than64 characters and invalid values are not cached. No caller access to current time, age decisions, proof receipts or data through this cache.

- [ ] TDD: first write and observe failing tests that repeated identical valid timestamp strings avoid repeated parsing and the cache never exceeds4096. Test fractional truncation, timezone offsets, numeric/bool/nonfinite/invalid/subclass behavior against the preserved parser contract. Use a real capture assessment demonstrating a warmed timestamp is still rejected as stale after advancing frame time; no mocked freshness comparison. Tests may spy on the parser solely to verify this pure performance contract.
- [ ] Apply only the exact minimal edit above, focused RED/GREEN and Python3.6 grammar/import compatibility. Run the whole Python discovery suite normal and optimized once on the final tree, freeze registered source hashes. Preserve unrelated untracked iCloud copies. Commit source/tests and report; no network/cloud/GitHub actions by task agent.
- [ ] Independent scoped task review, then root repeats exact-worker frozen901-frame calibration using production code without monkeypatches. Existing4s/5s limits stand; whole-branch review follows only if actual measured bound is feasible. Actual disabled proof still required.

### Task 5: Separate owned collector launch from original readiness

**Spec:** docs/superpowers/specs/2026-11-03-pgcat-startup-barrier-design.md; approved detailed proposal in this plan’s ignored workspace/startup-timeout-20261103-design.md.

**Files:** Modify only test-assets/traffic/pgcat_native_controller.py, pgcat_lane_protocol.py and pgcat_collector_control.py. Existing native controller/collector/gateway/causal/assessment tests as necessary; no new production path beyond those three.

**Interfaces:** Native launch output acknowledges actual retained guardian/collector and exact private result binding, without readiness. Native status consumes the exact authenticated original guardian result. All ready consumers use completed status; gateway capture budget continues using the original launch intent. Guardian invokes unchanged evidence semantics once within4s, retaining actual collector Popen and original capture end.

- [x] Write regression tests for actual startup delay shape, inherited actual process acknowledgement, authenticated later result and launch-only denial. Verify RED before production edits.
- [x] Implement the private owned launch channel and one-shot guardian readiness; bind original destination, key and identities without leaking authentication material or trusting sidecars.
- [x] Update typed launch/status validation, Journal admission/seal, immediate Mac ownership retention, worker dispatch and all restart/ingress/Gateway/Create readiness consumers. Preserve duplicate refusal and original run/plan/source anchors.
- [x] Cover original runtime fresh at readyAt but aged at later status, altered readyAt/bytes/inode/intent/process/binding, missing/partial/failed results, EOF/crash/collector exit, proof deadline at final publication, unchanged original capture end, and acknowledged-launch abort cleanup; retain foreign-sidecar rejection.
- [x] Run focused tests normal/optimized while iterating, then full traffic suite once on supported bundled Python; verify all27 production sources Python3.6 grammar/import, existing focused actual-process fixtures and diff hygiene.
- [x] Commit only task paths, self-review and write report including RED/GREEN evidence, exact test commands/counts, final27-source freeze and scope union15. Independent review required before publication/runtime. Root owns reviewed merge, exact-worker smoke/calibration, stale reservation recovery and fresh real disabled proof.

### Task 6: Completion-time evidence and action-bound Gateway read reuse

**Spec:** docs/superpowers/specs/2026-11-03-pgcat-action-observation-design.md. Owner approved the identical measured proposal Nov3.

**Files:** Modify only test-assets/traffic/pgcat_native_ingress.py, pgcat_native_closure.py and pgcat_native_controller.py. Modify existing related tests as required. No Go guard or new production module/registry/pin changes.

**Interfaces:** Ingress returns evidence assessed at completion after one bounded verified capture-tail advance and final original admission check. Closure owns private original-restart issuance keyed to exact WorkerGate/reads/PID/request/plan/prepared/credential/deadline, richer actual metadata fingerprints, bounded three-child GET multiplex and reuse of original proof bytes/timestamp only after fresh matching snapshots. Controller registers and removes eligibility only in actual worker_entry for native-disabled-restart and validates abort evidence after transport. All other contexts inspect freshly.

- [x] Write advancing-clock and terminal-freshness regressions before changing production; reproduce fresh runtime false-future refusal, aged final evidence, bounded healthy tail and changed original prefix/admission.
- [x] Implement final decision-time checks without widening3/15/30 freshness, losing failure flags, baseline restrictions or existing two precondition gates.
- [x] Write real owned-child multiplex tests for exact fixed GET commands, concurrent completion, oversized/malformed/failed/slow/interrupted reads and sibling stop/wait; implement same absolute deadline/2MiB output cap with actual Popen retention.
- [x] Add private full fingerprint of actual Secret/config, Deployment spec/version/status, Pod spec/owners/container/process/init identity; preserve existing external runtime schema and before/after checks.
- [x] Write source-registered issuance/reuse tests: genuine cold inspection once, fresh ordered warm snapshots, unchanged observedAt/provenance; forged/unregistered/template/abort/native/foreign object/PID/deadline/anchor contexts refuse; all drift terminally invalidates with no reissue after restoration.
- [x] Implement action-only registry lifetime and finally cleanup, with independent fresh abort observation and both idle checks. Retain both credential cleanup attempts and earlier SQL FAIL precedence.
- [x] Run focused normal/optimized tests during iteration, then one full final traffic suite on bundled Python. Check27 registered sources against Python3.6 grammar/import and unchanged inventory; diff hygiene, scope and secret-output self-review.
- [x] Commit only task source/tests/docs; generate action-observation-source-freeze.json and report RED/GREEN, interfaces, commands/counts, concerns and exact-worker offline test selections. Independent review gates publication.
- [ ] Root stages reviewed bytes and measures cold+actual warm-snapshot read-only composition (templates remain ineligible for cache), exact worker offline issuance/ownership tests, replay/prepared overhead and conservative unmeasured PATCH/transport reserve. Require demonstrated restart5s, abort3.5s worker/4s transport margins before another actual disabled proof; no inference of PASS from proxy timing. Root alone owns cloud access, PRs and actual proof.

### Task7: approved client metadata batching amendment

**Spec:** `docs/superpowers/specs/2026-11-03-pgcat-client-read-batching-design.md` (owner approvedNov3). This is one bounded source task; earlier tasks are complete and must not be repeated.

**Files:** existing `test-assets/traffic/pgcat_native_controller.py`, optionally `pgcat_native_closure.py`; existing `test_pgcat_native_controller.py`/`test_pgcat_native_closure.py`. No other production paths.

**Interfaces:** `FixedWorkerReads.live_clients()` continues returning exactly `{deployment,replicaSets,pods,roster}`. Add private `FixedWorkerReads._client_metadata()` returning `(deployment_dict,replicaset_items,pod_items)` after the fixed three-child group completes. If the existing owned-read transport is shared, keep its private selector limited to the two exact Gateway/client groups; callers cannot supply arbitrary argv. Preserve `FixedGatewayAdapters._metadata_objects()` return order/projection and before/after checks.

- [ ] Write failing module/discovery tests for fixed three-GET overlap and same projection; foreign/missing/malformed metadata; original deadline/credential drift; failed/oversized/slow/interrupted child cleanup and no partial acceptance. Use actual disposable children for ownership/overlap boundaries. Keep existing live-client ownership/rollout/starting-Pod tests.
- [ ] Run focused tests RED, preserving fixed failure evidence; do not rerun the completed1227-test baseline merely to begin.
- [ ] Implement minimal private fixed-group batching in the allowed modules, preserving exact existing argv/selectors/env, all validation, original absolute deadline and2MiB bounds. Pod HTTP reads, Gateway config reads, snapshots/gates/effects stay sequential. No source cache or lifecycle change.
- [ ] Run focused normal/optimized tests GREEN; final full traffic once;27-source Python3.6 grammar/import, diff/scope/freeze checks. Preserve prior085d/807d freezes and write a new distinct `client-read-batching-source-freeze.json`.
- [ ] Commit only task paths, report exact interfaces/RED/GREEN/final verification and offline worker-safe test selection; release source/index ownership. Independent task/final reviews and exact-head CI precede merge.
- [ ] Root: actual worker offline tests and fresh read-only timing with all missing costs and write/receipt reserve; only if adequate margin, one actual closed disabled proof and its complete cleanup/qualification, then the authorized full2→4→2 flow. Source completion alone is not runtime completion.

Task7 review focus: exact named versus selector-bound commands; no arbitrary command admission; original deadline survives shared-helper refactor; every child is stopped/reaped after another fails; Gateway before/after and live-client ownership behavior stay unchanged. Constraints from the amendment/spec and this plan's Global Constraints all apply.
