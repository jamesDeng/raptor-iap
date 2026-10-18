# Vben Admin Implementation Plan

**Goal:** Replace the existing Admin page renderer with a Vben-based frontend while preserving the hostname and Go backend.
**Architecture:** Vue SPA embedded by the existing Admin binary; existing same-origin API proxy and backend contracts remain authoritative.
**Spec:** ../specs/2026-10-18-vben-admin-design.md
**Execution:** Inline, with continuous execution and merge authorized by the owner.

## Constraints and review focus
Preserve existing routes, cookie sessions, CSRF, administrator denial, secret clearing and model policy version checks. Test expired sessions, route changes during pending requests, stale model policies, missing assets, and environment codes containing dots. Keep upstream attribution and exact source provenance.

## Tasks
- [x] Scaffold minimal upstream Vben workspace and Raptor app; pin dependencies and build.
- [x] Add failing API/session and browser acceptance tests; implement cookie/CSRF client and all existing Admin page workflows.
- [x] Add failing Go routing tests, replace old page renderer with embedded SPA and preserve API proxy/legacy hashes.
- [x] Integrate Docker and CI build/asset verification; run existing regression suites and production build.
- [x] Obtain independent review, resolve material findings, open PR, wait for checks and merge.
- [ ] Publish image, pin it through GitOps PR, verify live routing and pages, document evidence and limitations.

## Execution evidence

- Existing Go-rendered pages and API contracts were inspected before replacement. New Go SPA routing tests failed against the old renderer, then passed against the embedded SPA.
- 4 API/session unit tests, TypeScript checking and production build passed. Two consecutive builds produced identical asset checksums after disabling automatic Tailwind scanning of generated assets.
- 48 existing platform JavaScript tests passed. The full Raptor Go suite and vet passed against an isolated temporary localhost PostgreSQL instance.
- Independent review found a pending-save navigation race. Regraded as Important because a late redirect can discard context in another editor. The browser regression failed before the active-component guard and passed afterward. No review findings remain deferred.
- Browser acceptance covers all migrated workflows with synthetic API fixtures against the compiled Go server and production CSP. Desktop/mobile screenshots were inspected; responsive navigation was corrected.
- Current worker kubeconfig is expired. Do not transfer a replacement without the specifically required authorization; use public browser verification and GitOps evidence for this release.

- Source migration merged in PR #247 as `0728b379554339bfa0e3f24b99752eb81e86f458`; all required checks passed. Publication run 39120891008 passed, including published image packaging checks. Raptor digest `sha256:aef7a42ace94d70f354ca63f470ac98f55124ee53317478d81fc5b84717e97d6` was read from the completed publication job through the GitHub connector.
- The rdev release preserves the gateway image, hostnames and backend routing. The prior release source revision is an ancestor of this image source, preserving newer platform activity changes.
