# Infra API authenticated reads

## Verified local setup

Go 1.27.1 (module minimum 1.25), Serverless Devs 3.1.10, fc3 component 0.1.25, Terraform 1.13.3 and aliyun/alicloud 1.293.0. The package builds CGO-free Linux x86-64 code. FC runtime is custom.debian10 with port 9000, 512 MB, 30 seconds, instance concurrency 2 and no provisioned warm reservation. Caller credentials are injected privately outside the code package.

Serverless Devs owns the function and its `authType: function` HTTP trigger. Terraform owns read/invocation RAM roles, API group and three GET routes. It explicitly selects FC3 HttpTrigger and forwards `X-Infra-Authorization`. Gateway authentication is handled by the application Basic Auth header; its ANONYMOUS API setting does not make the FC trigger anonymous.

## Required deployment preflight

Use the existing `infra-ops-poc` operator profile privately. Confirm STS account, full September/October bills, delayed costs and balance. Select the existing Singapore VPC_SHARED gateway instance; this module does not buy an instance. Keep at most RMB20 reserved for the short probe inside the cumulative RMB1000 POC ceiling.

The account's 2026-10-05 metadata reports a VPC_SHARED instance, CNY1000 available and CNY0 currently billed. This is a point-in-time observation, not a hard spending limit. The [China-site traditional gateway price](https://help.aliyun.com/zh/api-gateway/traditional-api-gateway/product-overview/serverless-instance-1) lists CNY0.06/10,000 calls in the first charged tier and Singapore CNY0.75/GB; do not apply the separate new-edition USD/hour quote to this observed shared edition. At most 20 probe requests and 1 MiB response traffic are negligible, but verify the account's selected-edition quote before apply.

HTTPS is a separate deployment prerequisite. Bind an owned domain, configure CNAME and a valid certificate using [Aliyun's HTTPS procedure](https://help.aliyun.com/zh/api-gateway/traditional-api-gateway/getting-started/access-a-domain-name-over-https). The group's generated subdomain output is not proof of TLS. No domain was found in this account's DNS inventory; an externally owned domain may still exist. Do not send caller credentials over plain HTTP or bypass TLS validation.

Keep private execution inventory under ignored `.raptor-local/infra-api-read`, directory0700/files0600. Record the authenticated account, region, source commit, exact owned resource IDs, creation acknowledgement, tool owner and deletion confirmation as each change settles. An uncertain create outcome blocks retry/replacement/deletion until reconciled; never infer ownership from names alone.

Read/invocation roles are separate. Account-level RDS listing/tag reads require broad Resource scope; code additionally verifies STS account and exact env/code tags. ESS reads are restricted to scaling groups in the selected account/region. ACK kubeconfig permission is added only for an explicit cluster. Kubernetes get/list Deployment, ReplicaSet and Pod RBAC must be granted separately once the actual ACK cluster exists.

## Build and probe

Create a private scope array with `code`, `accountId`, `region` and optional `clusterId`. Build using `make build INFRA_SCOPE_FILE=/private/path/scope.json`. Run `s verify` with the pinned component before deploying. Supply only private runtime credentials/profile configuration; do not commit rendered YAML, logs, Terraform state, credentials or certificate private keys. Serverless Devs configuration validation here used synthetic secrets, not a live deployment.

Once protected FC and verified HTTPS ingress exist, the probe config contains `gatewayUrl`, `functionUrl`, `envCode`, `username`, `password`. The file must be private. Run `python scripts/read-probe.py --config /private/path/probe.json` inside the sandbox. It tests correct/missing/wrong caller credentials and direct unauthenticated invocation. It prints only safe checks, never credentials/provider bodies. Health alone cannot pass. ACK acceptance remains false until the Terraform platform and actual application deployment exist.

Delete only acknowledged temporary resources using their owning tool and exact recorded IDs. Do not delete the pre-existing shared instance, OSS/Volume, operator profile or retained OAuth state. Check absence independently after deletion and retain unresolved cleanup inventory. This read slice does not introduce a teardown command that can blindly destroy resources.
