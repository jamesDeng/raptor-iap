# Bounded proposal: prove post-restart connection observations

Status: proposal for owner review, not implemented or runtime admission.

## Problem and verified facts

PR #320 is merged and its source tests/reviews pass. Native entry deliberately refuses before enablement/Create because the retained assessment compares AX guest event times with worker/Prometheus times without a supported clock relation. Existing successful read-only qualification does not prove replacement.

On Nov2 a bounded worker-local read confirmed the actual Prometheus image is the existing pinned image, version3.15.0; /status/tsdb exposes headStats.maxTime; an instant query containing a range selector returns matrix samples with original timestamp/value pairs. The Gateway was1/1 ready and replacementEnabled=false. Exact sanitized receipt:2026-11-02-timing-readonly-probe.json. Both temporary operator credential files were deleted and absence verified. No rollout, app restart, flag change or fleet mutation occurred.

## Recommended approach

Use causal ordering in the existing private proof path, rather than add a clock service or change the normal agent MCP/API. During the released scale-down approval wait:

1. Observe the exact one-generation app rollout and the two exact successor processes using the existing complete retained capture/ownership checks. Keep the request on the same released waiting_approval attempt.
2. Read the fixed Prometheus source's current TSDB head maximum timestamp after that observation. Pin the source's Deployment/Pod/container/image, process start, loaded configuration hash and configuration incarnation; direct all reads to that exact Pod. Inspect only the existing fixed source and reject unexpected scrape configuration or source changes. Raw configuration remains memory-only.
3. Require THREE distinct complete finite direct scrape samples per retiring target after the watermark, using original timestamp/value pairs from a bounded range-selector response. Verify a single active loop hash per accepted full target/address/series identity, one fixed PgCat pool, honor_timestamps=false, scrape-on-shutdown=false, and no remote-write/rule/other job path capable of writing the accepted raw families. The pinned revision serializes pool sync and waits for removed loops; at most the old and incoming loop can have pre-watermark scrapes still in flight. Therefore at least one of three complete samples per target starts after the watermark. Do not assume numerical timestamp order identifies which one under a backward clock. Delayed shutdown StaleNaN markers cannot qualify. Reject historical replay, duplicate/missing samples, partial families, changed source/config, nonfinite values, unknown pool/user groups, up!=1 or deadline expiry.
4. Retain a private, original read receipt inside the existing sealed ingress witness, bound to the exact request/attempt/approval, rollout generation, successor identities and original node pair. Re-read the same released wait after the observation. For each node, require full active+idle+waiting coverage across all baseline or discovered pool/user groups in EACH of the three samples. Take the MAXIMUM complete connection total across that node's three samples, then SUM those maxima across BOTH retiring nodes; require<=1. This conservative bound avoids selecting a supposedly latest sample that could be pre-restart under a backward clock. Later discovered groups invalidate incomplete earlier witnesses. Preserve existing sample-age/coverage/traffic-gap deadlines. Approval can follow only after a valid witness; the later scale-in must belong to the resumed attempt and exact approved action.
5. The final assessment verifies that causal chain, rather than using cross-machine event timestamps to establish this ordering. It does not suppress SQL failures, missing samples or the remaining evidence/cleanup checks. The disabled proof exercises the same source identity/watermark/complete scrape mechanism after its real restart, without requiring the unchanged original fleet's connection count<=1.

The TSDB maximum prevents a backward clock from merely walking through previously stored samples. Source incarnation/configuration checks prevent cross-process or altered scrape behavior being treated as one sequence. Exact semantics and regression cases must be checked against the deployed Prometheus revision before source admission opens. Any unsupported behavior leaves admission closed.

## Alternatives and tradeoffs

- Keep the current guard closed: no new implementation, but the full test cannot pass.
- Add guest/worker/Prometheus clock probing: additional lifecycle and incarnation authority, and still no simple proof of continuous agreement. Not recommended.
- Recommended causal scrape witness: reuses the current Prometheus and private proof controller; waits for three qualifying real scrape samples and rejects uncertainty. No new cloud resources, IAM, public API or service/controller; normal agent stays on resource-based Infra MCP.

## Scope and gates

Estimated10–14 production paths plus focused tests: existing native ingress/controller/closure/assessment and source/admission manifests/registries, possibly a narrowly shared parser. Keep the existing collector transport, provider feed, fleet actions, public MCP contract and agent skill behavior. Do not edit the Go Gateway guard or add dependencies unless separately justified. Before implementation, prepare a precise path inventory; stop for owner review if this becomes a new architecture or exceeds this bound.

Test backward-clock historical replay, old/in-flight scrape, value/timestamp races, source restart/reload/swap, foreign/missing groups, combined threshold1 vs2, same-wait binding, disabled-proof parity and failure precedence. Use supported local Python and actual workerPython3.6; independent review; source freeze; planned-volume timing; coordinated existing GitOps release; one real disabled proof including120-second hold/restoration/both credential absence; then fresh native preflight and one2→4→2 attempt only if all gates pass.

Owner-authorized whole-POC policy ceiling is CNY3000 net. Use the operator's private daily billing receipt, retain the existing delayed-charge reserve, and complete a remaining-runtime/cleanup forecast before enablement; account observations are not published in this design. No additional billing read today. Min2/max4, desired>=minimum, exact decrease approval and combined retiring connections<=1 remain. Mutation cutoff21:30/work stop22:30 Shanghai.

## Primary references

- Prometheus HTTP API: https://prometheus.io/docs/prometheus/latest/querying/api/ (TSDB head maximum, matrix timestamp/value result, runtime information).
- Pinned scrape implementation inspected: https://github.com/prometheus/prometheus/blob/5241a27fe3c6983549fccc32f6e65917408c63cd/scrape/scrape.go (Sync/sync, restartLoops, run, scrapeAndReport, stop/end-of-run staleness).
- Builder check found the two-sample loop-replacement counterexample; this proposal incorporates its three-sample correction and conservative all-sample threshold. Builder check is design analysis, not runtime qualification.

Implementation details such as response bounds and receipt field names are the builder's choice within the existing fixed-read and source-verification conventions; they may not widen selectors, weaken freshness/identity checks or invent authority from summaries.
