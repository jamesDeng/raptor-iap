# PgCat complete observation and pre-model qualification preflight

Owner approved the bounded proposal on2026-11-01. Existing API/Gateway flow only; replacementEnabled=false. The prior unpushed two-worker adapter59e1afa is preserved on its branch and is not the implementation base. Current base01c48fcb44113d3d841200c1b5ccde3737fbf2e7 includes reviewed deployed314/315.

## Problem and authority

AX resize recovery/compatible guest verified. Eight sequential resource/metrics calls exceed20s; eight concurrent calls can exceed FC capacity (live instanceConcurrency2/reservedConcurrency2) and return429. Service-only two-worker whole observation18.897s omits agent matchProof; actual agent fleet5.450s vsservice2.931s means no proven native timing margin. Do not widen cloud capacity, deadline, freshness or scope.

## Complete existing-tool read

The existing resource MCP tool proxy_pgcat_metrics_get gains one finite series=clients selection. Same requestId/envCode/proxyCode selectors and existing pgproxyWorkload envelope. Existing eight single-series behavior remains compatible. The new selection is resource-only; do not add it to legacy physical-selector APIs. No new tool/route/arbitraryquery/URL/providerselector/cache.

New internal domain PgcatClientsObservation JSON has exactly series="clients", observedAt, fleet, backend, samples. samples has exactly up,upTimestamp,active,activeTimestamp,idle,idleTimestamp,waiting,waitingTimestamp, each a raw []PrometheusSample. Fleet/backend keep their actual source observation timestamps; all raw metric numeric strings/labels/value timestamps are preserved. Outer observedAt equals metrics.observedAt, neither substitutes for raw timestamps.

Use the existing request authorization and complete resource proof checks. Resolve mapping and membership once per bundle; reuse existing provider validation for fleet/backend. Initialize one private ACK Prometheus client per bundle; same fixed Service/namespace, no redirects, bounded reads, exactly eight fixed expressions, max2private queries in flight, stop queued work/cancel peers on failure/deadline. No retries/partial result. Revalidate the complete bound identity (inclactualDB/appUID) before exporting. Initial/final proofs must both fit the same original20s Gateway observation budget; sourcefreshness15s/responsefreshness30s/allpool-user-nodecoverage remain authoritative. Server bounds the combined encoded MCP response including structured/text duplication; client262144byte limit stays unchanged, reject oversize without truncation.

Gateway packages one logical clients read only for metrics_read kindpgcatClients. Its existing fleet/backend reads for that observation obtain bundle.fleet/backend; individual series get bundle.samples, all within that invocation. Existing resource envelope checks plus exactbundlekeys/rawcoverage validators reject malformed/foreign/stale data. Do not modify observation-tools.mjs or pgcat-metrics.mjs validation behavior. Other tools unchanged; no cross-observation reuse.

## Pre-model actual agent preflight

Use the already registered ObservationProxy entry: prepareObservation returns sanitized transport config, not raw private material. Factor the current HTTP execution guards into a shared internal guarded executor. Preserve HTTP auth/input checks,20s timeout/busy gate, minimum source/access/attempt expiry, entry pre/post lease/cancel checks, active-entry check, JSON/size check and every material.Secrets check. Internal fixed preflight finds exactly one entry matching the full AttemptBinding AND issued access (not requestIDalone); executes only metrics_read {kind:pgcatClients}, discards bytes. No route/token/registry/authority change.

AXLive.Start calls this after observation registration and checkpoint validation, before model leasing/axRuns/sandbox intent/AXensure, only when executionMode=qualification. Repeat on resumed qualification Start as a feasibility check, never inject its data into trace/baseline/result or replace fresh post-approval model reads. Missing private proxy/preflight configuration fails closed. All other operations and replacement admission unchanged. Failure uses existing RecoveryRecord/ObservationProxy.Remove/Access.Revoke paths; no fabricated verified checkpoint. Tests prove zero modellease/ensure and runtime-slot release on failure, including cancellation/lease loss.

## Scope and release

Production scope11files: Infra domain/proxy_observation.go; provider/{pgcat_observation,prometheus,proxy_observation}.go; api/{server,operations,resource_operations,resource_mcp}.go; harness/gateway-observation-runner.mjs; Gatewayruntime/{observation_proxy,ax_live}.go. Existing test/contract files accompany them. No >12productionfile expansion without surfacing it. No Terraform/IAM/FCcapacity/resource/collector/AXguest/skills/modelpolicy changes.

TDD, independent task reviews, final integration review, required CI. Deploy additive Infra code using existing owner first, verify no config drift; then immutable Gateway image pin through existing GitOps. Actual preflight must pass before model use. One read-only qualification then verify baseline persistence, exactharmless approval,pause,resume,freshfullreads and finalcleanup. Only then nativequalificationPASS; realreplacement/trafficacceptance remain notassessed/disabled.
