# Runtime binding and enablement

There is no released mutation wire contract established by these assets. The names `proxy_inventory`, `proxy_scale`, `proxy_node_protection`, `proxy_deregister`, `application_restart`, `proxy_metrics`, and `operation_status` in the planning document are proposals, not callable tool names. Do not invent their HTTP paths or call cloud APIs in their place.

Before runtime enablement, central review must bind actual published MCP/HTTP schemas for request/attempt scoping; discovered group/node/Deployment identities; exact desired-capacity inputs; approval grant/expiry/consumption and logical-operation retries; provider status/unknown reconciliation; fresh healthy registration and ESS desired capacity; Prometheus queries with complete expected node/pool inventory and source timestamps; cancellation/pause/resume ownership. Read authoritative schemas at the pinned deployment/skills revisions. These bindings must remain compatible with server-side checks.

The test stack also requires qualified secret delivery/TLS/bootstrap image and private scrape inventory; ESS/NLB behavior after manual deregistration; application-to-proxy association; externally retained full-window traffic evidence; and centrally approved cost/live rollout. The disabled Terraform instance and local tests satisfy none of those live requirements automatically.

Whole-folder skills releases use immutable `skills-vMAJOR.MINOR.PATCH` tags backed by exact commit SHAs. Requests store both tag and resolved SHA; older versions remain selectable and new tags cannot change an existing request. This branch creates no tag or runtime enablement. A skills change uses the selected interruption/next-pause behavior and preserves the same request history; no silent hot reload.
