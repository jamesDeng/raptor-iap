# Infra API authenticated reads

## Recorded local setup and current source surface

Go 1.27.1 (module minimum 1.25), Serverless Devs 3.1.10, fc3 component 0.1.25, Terraform 1.13.3 and aliyun/alicloud 1.293.0. The package builds CGO-free Linux x86-64 code. FC runtime is custom.debian10 with port 9000, 512 MB, 30 seconds, instance concurrency 2 and no provisioned warm reservation. Caller credentials are injected privately outside the code package.

Serverless Devs owns the function and its `authType: function` HTTP trigger. Terraform owns read/invocation RAM roles, API group and Gateway routes. Its retained GET identity route is legacy: the active resource handler returns NotFound there. Current reads use deployments/status and POST `/mcp`; restart and conditional proxy command routes remain opt-in. See the [current endpoint inventory](infra-api-resource-endpoints.md). It explicitly selects FC3 HttpTrigger and forwards `X-Infra-Authorization` through PASSTHROUGH mode. Do not add a mapped request-parameter block in this mode. Gateway authentication is handled by the application Basic Auth header; its ANONYMOUS API setting does not make the FC trigger anonymous.

## Required deployment preflight

Use the existing `infra-ops-poc` operator profile privately. Confirm STS account, full September/October bills, delayed costs and balance. Select the existing Singapore VPC_SHARED gateway instance; this module does not buy an instance. Keep at most RMB20 reserved for the short probe inside the cumulative RMB1000 POC ceiling.

The account's 2026-10-05 metadata reports a VPC_SHARED instance, CNY986 available and CNY14 currently billed. This is a point-in-time observation, not a hard spending limit. The [China-site traditional gateway price](https://help.aliyun.com/zh/api-gateway/traditional-api-gateway/product-overview/serverless-instance-1) lists CNY0.06/10,000 calls in the first charged tier and Singapore CNY0.75/GB; do not apply the separate new-edition USD/hour quote to this observed shared edition. At most 20 probe requests and 1 MiB response traffic are negligible, but verify the account's selected-edition quote before apply.

HTTPS is a separate deployment prerequisite. Bind an owned domain, configure CNAME and a valid certificate using [Aliyun's HTTPS procedure](https://help.aliyun.com/zh/api-gateway/traditional-api-gateway/getting-started/access-a-domain-name-over-https). The group's generated subdomain output is not proof of TLS. The owned `raptor-iap.top` domain and issued certificate were verified in the live probe. Its temporary hostname binding was removed after testing. Do not send caller credentials over plain HTTP or bypass TLS validation.

Keep private execution inventory under ignored `.raptor-local/infra-api-read`, directory0700/files0600. Record the authenticated account, region, source commit, exact owned resource IDs, creation acknowledgement, tool owner and deletion confirmation as each change settles. An uncertain create outcome blocks retry/replacement/deletion until reconciled; never infer ownership from names alone.

Read/invocation roles are separate. Account-level RDS listing/tag reads require broad Resource scope; code additionally verifies STS account and exact env/code tags. ESS reads are restricted to scaling groups in the selected account/region. ACK kubeconfig permission is added only for an explicit cluster. Kubernetes get/list Deployment, ReplicaSet and Pod RBAC must be granted separately once the actual ACK cluster exists.

## Build and probe

Build using `make build`; active templates package only the `server` binary under ignored `.build-resource/`. No scope file is loaded or bundled. Configure the fixed HTTPS `RAPTOR_CATALOG_ORIGIN` and existing private `RAPTOR_SERVICE_USERNAME`/`RAPTOR_SERVICE_PASSWORD` credentials for catalog-only context reads. Catalog environment fields and the logical `proxyApplications` relationship must be present and reviewed; see the [current runtime requirements](../../components/infra-api/README.md). Backend startup no longer uses a replacement-scope file. This source refactor remains held for coordinated consumer migration, least-privilege NLB read review and qualification; the historical deployment/probe instructions below are not authorization to deploy. Run `s verify` with the pinned component before deploying. Supply only private runtime credentials/profile configuration; do not commit rendered YAML, logs, Terraform state, credentials or certificate private keys. Deploy with an explicit `s deploy -a "$INFRA_DEPLOY_PROFILE"` profile selection. The live probe used a dedicated RAM deployment identity scoped to the exact function and its execution role; it was revoked after acceptance. Existing operator OAuth-derived STS credentials failed for FC with the tested clients, so verify the chosen deployment identity before creation.

Once protected FC and verified HTTPS ingress exist, run the existing logical MCP probe from `components/infra-api`:

```sh
go run ./cmd/mcp-probe
```

Supply `INFRA_MCP_URL` (HTTPS `/mcp`), `INFRA_USERNAME`, `INFRA_PASSWORD`, `INFRA_ENV_CODE` and `INFRA_APP_CODE` privately. It validates the current logical tool catalog (including the typed status schema), calls application deployment discovery, refuses redirects and prints no credentials. Actual status readiness must be checked separately. The old `scripts/read-probe.py` calls the removed cloud identity operation and is historical; it is not current acceptance. Account/region verification occurs inside provider resolution. Separately verify protected direct-trigger and missing/wrong/revoked/foreign credential denial with the selected deployed package. A quick probe is not Raptor qualification: actual catalog/workload identity, complete metric freshness, baseline continuity, harmless approval/pause/resume and cleanup still need fresh live evidence. Health alone cannot pass.

Source tests and binary packaging do not establish deployed readiness. Release requires coherent reviewed Raptor/Gateway/harness/guest/FC receipts, independently verified catalog relationships and effective permissions. The inspected FC policy lacks `nlb:ListServerGroups` and `nlb:ListListeners`; exact servergroup ARNs and account/region `listener/*` respectively require a reviewed component Terraform delta, sanitized PR plan comment and reviewed apply. No grant is part of this source change. Keep replacement disabled and preserve idle/rollout gates. Full 2→4→2 remains NOT PASS; immutable direct-v2 replacement skills require a compatible new release and native completion gates remain separate.

Delete only acknowledged temporary resources using their owning tool and exact recorded IDs. Do not delete the pre-existing shared instance, OSS/Volume, operator profile or retained OAuth state. Check absence independently after deletion and retain unresolved cleanup inventory. This read slice does not introduce a teardown command that can blindly destroy resources.
