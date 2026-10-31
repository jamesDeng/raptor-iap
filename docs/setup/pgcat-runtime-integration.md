# PgCat runtime integration

Proxy commands remain disabled unless `INFRA_ENABLE_PROXY_COMMANDS=true`.
All resource reads require fixed HTTPS `RAPTOR_CATALOG_ORIGIN` and existing
`RAPTOR_SERVICE_USERNAME`/`RAPTOR_SERVICE_PASSWORD` supplied privately. Proxy
commands additionally require `RAPTOR_APPROVAL_ORIGIN` and existing command
dependencies. Historical POC origin `https://api.rdev.raptor-iap.top` is not a
current deployment receipt.
Keep credentials out of source, Terraform variables/state and public logs.
`INFRA_ENABLE_RESTART` remains independent.

Catalog-only Raptor context supplies canonical resource relationships, including
`proxyApplications` with exactly one application code per proxy. Infra freshly
resolves provider tags/relationships and pins the complete private realization;
admission requires exactly one client Deployment. No packaged scope mapping
supplies runtime authority. Model inputs are logical env/proxy codes, finite
read options, opaque workload keys and proxy-bound observed node IDs; returned
provider identities remain evidence. No model-supplied endpoint or PromQL is used.
Current bootstrap expects pool `test` and user `poc_app`; changing this bootstrap
requires updating the observer before enabling it. All current ESS nodes,
including protected/deregistered nodes, need complete fresh scrape coverage.
Metrics inventory is explicitly refreshed with the provider inventory helper;
it is not automatically reconciled. Refresh it after membership changes.

Apply the namespaced metrics Role and a RoleBinding for the verified RAM RoleId.
It grants GET of only `http:rdev-test-metrics-metrics:http` on services/proxy in
raptor-test. Prove using the actual narrowed role certificate that this GET
succeeds while other Service proxies and Secrets are denied. An administrator
probe cannot establish this property. Preserve existing read/restart bindings.

The Infra API Terraform module owns function permissions, gateway routes and
shared GitOps identity deployment supplements. Existing role/trust definitions
and state addresses stay unchanged. The existing state key remains
`rdev.ali/infra-api.tfstate`; use the shared accepted TableStore locks.
For first use only, bootstrap the module's four `gitops` policy/attachment
resources with a reviewed Terraform saved plan using the authorized deployment
identity. Do not grant these permissions with CLI/console. This resolves the
initial CI permission cycle; all subsequent runtime changes use PR plan/comment
and reviewed saved-plan apply. Set the private `TF_INFRA_STACK_INPUTS` GitHub
secret to the existing trigger_url and gateway_instance_id. The new workflow
uses the existing unapproved plan environment and reviewed apply environment.

Plan identity supplements can read component-owned roles/policies, the selected
API group and exact state object; they cannot mutate cloud resources or state.
Apply can change versions of the component-owned policies and create/modify/
deploy APIs only in the existing API group. This includes its own deployment
supplement policy: treat approved repository code and the apply reviewer as the
trust boundary. No RAM role creation, trust changes, direct ECS termination,
ASG RemoveInstances or resource deletion is granted.

ESS tag enumeration spans Singapore groups to detect ambiguous tag matches;
runtime instance reads and mutations pin freshly resolved exact group IDs.
Terraform permission resources remain explicitly component-owned configuration;
runtime discovery does not widen IAM. The inspected FC policy lacks the new
`nlb:ListServerGroups` read (exact owned servergroup ARNs) and
`nlb:ListListeners` read (account/region `listener/*`). Their reviewed component
Terraform delta, sanitized PR plan comment and reviewed apply are prerequisites;
LB request filtering is not an IAM condition and no grant is included here.
NLB health reads are scoped to the owning load balancer because the API's RAM scope is the load
balancer. ListServerGroupServers documentation uses both serverGroup and
servergroup ARN casing across references; both exact IDs are included for read
compatibility. Backend removal uses the documented lowercase servergroup ARN.
A nonempty NLB health NextToken refuses because the operation has no request
pagination token. Receipts establish submission, never completion.

Before enabling real mutations, deploy Raptor's additive approval migration,
verify exact claim/record service routes, private metrics and refusal cases,
and ensure no active shared request is disrupted by a rollout. Each new
scale-in must use a new exact in-product approval/action. Lost acknowledgement
consumes the action; reconcile provider state instead of replaying it.

Use the explicit `s.rdev-proxy.yaml` Serverless Devs package only after
qualification; it preserves the existing function, VPC and trigger and reads
approval service credentials from private deployment environment variables.
Both templates now require catalog origin and existing service credentials;
`s.rdev.yaml` retains read/restart behavior while the proxy template additionally
configures approval/command dependencies. Both package only
`.build-resource/server` and target the same function; do not deploy them
concurrently.

All proxy mutations also use a Raptor-owned durable fleet guard keyed by env and
ESS group. It excludes distinct operations across FC instances, including while
an asynchronous provider job is pending. Claim/record/resolve are service-only
HTTP routes, never agent tools. A later command reconciles a recorded intent
against a complete provider snapshot before taking a new guard. Guards have no
expiry. A process crash before recording or an uncertain call with no observable
effect requires operator reconciliation; never clear it merely because time
elapsed. Approved scale-in action consumption remains separate and permanent.

Terraform OSS backend initialization lists the exact existing `rdev.ali/`
workspace prefix. This reveals object names under that prefix; object contents
remain restricted to `rdev.ali/infra-api.tfstate`. A condition on the full state
object name cannot authorize the backend's workspace listing request.

Current source contracts are inventoried in [Infra endpoints](infra-api-resource-endpoints.md). Source fixtures/suites and binary packaging prove source parity only. Release requires matching reviewed Raptor/Gateway/harness/guest/FC receipts, actual catalog relationships/tags/UIDs and effective permissions, with replacement disabled and idle rollout readiness. Read-only qualification uses a new ordinary logical Raptor request, fresh metrics and a persisted baseline, then harmless `runtime.qualification-resume` approval, immediate pause, same-request resume and owned cleanup/token revocation. No source test is live qualification or latency evidence. Published skills remain immutable: current direct-v2 replacement content needs a compatible reviewed new release. Full 2→4→2 and native completion remain NOT PASS pending their separate gates.
