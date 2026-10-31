# Raptor resource transport contract

Source-only migration, 2026-10-31. Replacement remains disabled and full native
replacement/traffic acceptance remains NOT PASS. Existing completion/collector
stubs are unchanged. No new skill release, cloud request, IAM change or deployment
is established by these tests.

The normal observer MCP producer sends only requestId/envCode/proxyCode plus the
finite observation kind or metric series. Infra returns a resource-rooted
pgproxyWorkload of type ess. One strict adapter validates the resource code,
target database code, selected nested field and matching source time, then passes
the existing private fleet/backend/capacity or raw metric object to guards. It
never decodes an opaque workload key as an ESS group. The existing shared 20-second
deadline covers initialization, nested state reads, eight metric series, body
reads and SQL probes. No SDK/CLI/Prometheus fallback supplies proxy observations;
operator diagnostics remain explicitly separate. Existing traffic-client reads
continue using the established private collector path.

Raptor request_get advertises the exact application/database logical references.
Scoped deployments_list takes requestId/envCode/kind/code and returns the selected
resource and deployments. Proxy resourceId is empty; workloadKey is opaque and
separate. Application resourceId/UID and database RDS identity remain actual
private evidence. Status takes envCode/appCode with optional returned workloadKey;
restart requires requestId/envCode/appCode/workloadKey. Proxy writes use nodeIds
inside the authorized proxy namespace. Scale-in approval uses db-proxy.scale-in,
target {proxyCode}, and parameters {desiredCapacity}. Legacy provider selectors
are refused before transport, rather than silently stripped.

Every new issued proxy attempt requires a complete self-consistent canonical
resolution proof. The trusted issued scope is saved write-once in the existing
private attempts.binding JSON before runtime Start, without storing credentials.
Scalar binding retries preserve that member. Later attempts compare the complete
first proof; outcomes and recovery compare against the persisted proof. A legacy
or missing proof can be read/cleaned up but cannot authorize new execution. The
existing Pi session journal owns the original fleet baseline across attempts;
new reads cannot replace it. No new table, schema migration or baseline store is
introduced.

The existing observer deployment configuration remains an additional operator
fence for the established credential/kube/SQL preparation path. Its old identity
tuple must match the trusted issued proof; any configured new proof fields also
must match. Account, region, listener and actual database identity must match
before credential/material preparation. The configured tuple never supplies a
missing proof or acts as resource lookup authority. Removing that existing
collector preparation mechanism is outside this source migration.

Read-only qualification reports version 2 and transportContract
raptor-resource-v1. The current checks include actual scoped Raptor discovery;
cloud account verification remains an internal Infra invariant, not a fabricated
MCP check. Discovery evidenceMode is discovery and observedAt means transport read
time. Only dedicated source observations establish metric freshness. Private
Gateway results retain full proof; Raptor validates them before omitting newly
added account/region/listener/port/database-resource/digest proof scalars from the
public v2 qualification projection. Historical v1 results remain readable.

The immutable qualification skill's safety procedure is retained. A trusted
runtime notice/schema/read plan explicitly supersedes only its obsolete wire
selectors. The selected skill hash/tag does not change. The immutable replacement
skill's direct-v2 semantic contract is incompatible: it mandates provider-shaped
arguments and old scale-in approval. Both Native and AX runtimes check verified
pinned Git blobs before observation credentials, remote runtime provisioning or
model work, and return ResourceContractMigrationRequired. Cleanup revokes already
issued access and retains that explicit failure code. The checked-in candidate
also remains gated; no skills-v0.5.0 release is implied. A compatible replacement
skill/release requires separate review before live replacement can proceed.

Existing deployed source/image/hash receipts remain historical. This migration
requires fresh coherent image receipts after coordinated review, catalog and
permission prerequisites, and explicit live read-only qualification. Source tests
are not live qualification evidence. Actual call counts and latency for fresh
proof/metadata reads plus eight sequential metric series must be recorded during
that later qualification; no timeout or freshness bound was relaxed here.
