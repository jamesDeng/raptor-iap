# Read integration acceptance — 2026-10-05

Status: local implementation verified; live sandbox→gateway→FC acceptance incomplete.

The HTTP service, official cloud readers and Raptor reader have synthetic contract tests. They distinguish empty inventory, provider failure, unconfigured ACK, scope mismatch and changed workload UID. Kubernetes pod summaries require Deployment/ReplicaSet owner identity. Credentials are not forwarded across redirects. No runtime mutation adapter is added.

Package evidence: static ELF Linux x86-64 binary; Serverless Devs 3.1.10/fc3 0.1.25 schema validation; Terraform 1.13.3/alicloud 1.293.0 validates, with mocked ingress/account contracts. This does not prove effective RAM permissions or cloud ingress behavior.

Real read-only preflight through the existing OAuth profile verified STS identity, Singapore VPC_SHARED availability, CNY1000 available balance and CNY0 currently billed for September–October (0 and 3 product rows respectively). Delayed charges remain possible. DNS inventory returned zero domains. [Official HTTPS setup](https://help.aliyun.com/zh/api-gateway/traditional-api-gateway/getting-started/access-a-domain-name-over-https) requires an owned domain and certificate; these have not been supplied/verified. Therefore no cloud function, role, group, API or sandbox was created for this slice, and no cleanup is required for new cloud resources.

Pending: owned HTTPS domain/certificate, selected-edition account quote, protected FC deployment, positive/negative live sandbox probes and actual cloud cleanup receipt. ACK discovery/status acceptance also awaits the later Terraform foundation. The temporary-resource ownership and probe classifiers are tested locally; there is no fabricated live success receipt.

Final local gate before independent review: all three Go modules pass race tests and vet; 114 existing Python tests, 25 harness tests, 2 probe tests and 2 mocked Terraform runs pass; both Terraform configurations validate and whitespace checks pass. No remote CI or live Infra API acceptance has run.
