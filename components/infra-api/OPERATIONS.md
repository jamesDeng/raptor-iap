# Runtime operations integration contract

Status: local implementation only. `cmd/server` defaults to read-only `api.New`; `INFRA_ENABLE_RESTART=true` enables only Deployment restart through both HTTP and MCP. No operation route is deployed and no cloud permission or shared configuration changed.

## Current POC authentication decision

Owner decision on October 7, 2026: retain HTTPS Basic Auth alone for the POC. Signed request context and per-attempt credentials are deferred future options, not integration prerequisites. Basic Auth identifies the service caller but does not prove per-request/object/attempt authorization. Keep provider account/environment/resource-identity checks and exact scale-in approval/metrics enforcement. Tool visibility is not an authorization boundary. This documentation update does not enable operations or deploy anything.

## Command transports and request authorization

`api.NewWithCommands(reader, envs, username, password, authHeader, commander, authorizer)` adds typed shared HTTP/MCP dispatch. All mutations require an injected trusted authorizer. It receives authenticated Basic username, operation path and validated selectors; submitted requestId alone is never proof. Bodies reject unknown/duplicate keys, missing required fields, wrong scalar types, duplicate node IDs and sizes over 8 KiB. Injected authorization also applies to shared reads. Mutation MCP catalog exposure additionally requires `CommandToolVisibility.ExposeCommands()`; it controls visibility only, not call authorization.

| HTTP POST | MCP tool | Required fields |
|---|---|---|
| `/v1/deployment-restart` | `deployment_restart` | requestId, envCode, appCode, clusterId, namespace, name, uid |
| `/v1/db-proxy-scale` | `db_proxy_scale` | requestId, actionId, envCode, proxyCode, groupId, integer desiredCapacity |
| `/v1/db-proxy-node-protection` | `db_proxy_node_protection_set` | requestId, envCode, proxyCode, groupId, unique instanceIds, boolean protected |
| `/v1/db-proxy-node-deregistration` | `db_proxy_nodes_deregister` | requestId, envCode, proxyCode, groupId, serverGroupId, unique instanceIds |

`QuestionAuthorizer` accepts a trusted per-call verifier of immutable request/attempt identity, current active state, environment and application. It allows only identity/application discovery/application status; providers additionally enforce deployment UID/label/cluster. Concrete signed-context or per-attempt credential verification remains unimplemented. Existing `api.New` keeps its read-only compatibility behavior.

## Restart and proxy runtime boundaries

The object returned by `provider.New` implements `commands.Restarter` and can be cast when wiring reviewed runtime. Restart validates Deployment UID/application label/configured cluster and patches only pod-template annotation with UID/resourceVersion preconditions. Conflict returns TargetChanged; lost acknowledgement returns SubmissionUnknown without retry. Existing status reports current state, not restart-attributed history. No operation database/ID was added.

`commands.Runtime` implements individual command policy using injected Backend, Metrics, Approval, Claims and Restarter. No concrete proxy cloud Backend is supplied. Backend must resolve complete authoritative groups tagged by selected env/proxy in configured account/region, desired/min/max, every member/protection state, configured traffic backend identity and current membership/health. Incomplete pagination or unknown health must refuse. Runtime pins exactly one group and re-reads before mutation; this is not an atomic cross-provider/cross-replica guarantee.

Protection and deregistration require membership checks and no human approval. Deregistration retains healthy registered group members > ESS desired/2 after excluding requested nodes, and removes routing only. Provider connection/draining behavior remains unverified. Scale direction uses current desired; no-op submits nothing and scale-out adds no approval/metric gate.

Scale-in checks exact Raptor approval, fresh zero frontend clients on every unprotected node and a durable atomic Claims boundary. It deliberately does not check routing exclusion. Claims must allow one action submitter, deny duplicate/unknown claims and durably record submitted/unknown outcomes. Lost receipt recording returns SubmissionUnknown and cannot release the claim for automatic replay. A metrics check that expires during claiming is revalidated immediately before submission, recorded as `not_submitted`, and refused. No Raptor claim endpoint/schema is implemented; existing approval-check alone is not replay-safe.

## Metrics and approval clients

The Prometheus adapter uses a fixed HTTPS origin, denies redirects, bounds response size/time and queries at one fixed evaluation time:

- `raptor_pgcat_connected_clients{env_code,proxy_code,ess_group_id,ecs_instance_id}`: complete frontend connected clients including idle, excluding backend pool connections.
- `raptor_pgcat_connected_clients_observed_at_seconds` with identical labels: successful complete source observation time, never refreshed from cached/error data.

Each unprotected node needs exactly one finite nonnegative integral count and timestamp; maximum source observation age is 45 seconds and future tolerance is 5 seconds. Positive counts refuse. These implement reviewed draft defaults, not verified PgCat native/exporter behavior. Scrape time never substitutes for source observation time.

Raptor client uses fixed HTTPS/Basic authentication and `POST /v1/approval-check` with interface `ess.scale-in`, exact request/action/env, target `{proxyCode,groupId}`, parameters `{desiredCapacity}`. Duplicate/malformed responses, redirects, timeout and anything other than explicit allowed=true fail closed. Check is not approval consumption.

## Central integration dependencies

- Basic Auth alone is selected for the POC. Do not require signed request context or per-attempt credentials to proceed. Preserve the injectable authorization boundary for a future request-scoped implementation.
- Supply Raptor durable action claims/receipts, or explicitly choose the alternative replay-risk contract. Current runtime requires claims for scale-in.
- Supply actual ESS/traffic Backend, immutable mappings, exporter coverage and provider health/draining semantics.
- Choose cross-instance serialization/lock contract; one active sandbox does not serialize all callers.
- Review function split/role permissions, ACK Deployment patch RBAC, command route module and private connectivity/secrets. No widening of the read role is included.

Local unit and HTTP/MCP tests use provider fixtures. Live acceptance, cloud apply, permission grants, deployment, spending and shared merge remain with central coordination.

## Review limitation

One minor review finding is deferred: proxy policy `IdentityChanged`/`ScopeMismatch` errors currently become `ProviderUnavailable` in transport error mapping. Calls still refuse without mutation, but corrective diagnostics are less precise. Durable concurrent claim semantics and actual cloud backend behavior cannot be verified until those external adapters exist.

## Restart startup wiring

Set `INFRA_ENABLE_RESTART=true` only in a centrally reviewed deployment configuration. Missing/false retains the existing read-only startup; invalid boolean configuration refuses startup. Restart mode uses the existing Basic username/password and configured auth header, validates resources in the provider, exposes the three read tools plus `deployment_restart`, and rejects proxy command routes. Signed request context is not required. Request IDs remain correlation inputs rather than proof of request-specific authorization. No shared function configuration, role, API Gateway route or ACK RBAC was changed by this local wiring.
