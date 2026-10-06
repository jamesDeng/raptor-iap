# rdev Kong ingress

Prepared configuration, not installed or publicly accessible yet. Uses the official ingress0.24.0 chart, bundled kong3.2.0, Kong OSS3.9.3 and KIC3.5.3. Upstream archive SHA256670053e7ae58f0b09951f9edc3ab4dea6cab59c970efb6080b3082f63b4fc1f2 matches official chart index. Public image retrieval and live readiness still require verification.

The wrapper keeps PostgreSQL explicitly disabled in both subcharts. One proxy and one controller request a combined200mCPU/384MiRAM. Proxy exposes443 only. Admin services remain ClusterIP; the controller watches kong-system and raptor-system, including their TLS and other Secrets, with additional cluster metadata/Kong configuration access. Admission webhooks are disabled. Review rendered RBAC before installation; no implicit permission approval.

Terraform owns a separate Internet-facing CLB in Singapore A/B. The existing lb-t4nju4n311gx4yeqhehjo is ACK's private API load balancer and must never be reused. enable_kong_ingress and publish_kong_dns default false. ACK manages proxy listeners/backend groups; Terraform does not. Helm rejects an empty exact load balancer ID. The ID guard prevents accidental automatic creation; the operator must still verify actual ownership.

Public routing uses raptor.rdev.raptor-iap.top for frontend and api.rdev.raptor-iap.top for /mcp and /v1. Application authorization remains authoritative. Admin UI and internal service endpoints have no ingress. Both hostnames require a valid certificate stored privately as raptor-public-tls in raptor-system. Certificate retrieval queried October6 returned no matching orders; this is not proof that no certificate exists elsewhere. Do not publish with Kong's default/self-signed certificate.

For public deployment set RAPTOR_SECURE_COOKIES=true (rdev Helm values enable it). This explicit policy sets secure login/logout cookies behind TLS termination, without trusting arbitrary X-Forwarded-Proto headers. Publish a new image containing the fix before public acceptance; earlier image digests do not contain it.

Argo's separate rdev-kong project allows only the two namespaces and chart-required cluster kinds. Its two sources reconcile the chart and exact routes.yaml. Pruning starts disabled. Applying an Application before filling the owned ID intentionally fails rendering.

Live price RFQ request01A10F75-74B3-593B-835E-F2529C7D99C1 returned CNY0.04/hour InstanceRent and CNY0.75 usage InternetTrafficOut. It omitted explicit LCU/new base-instance charges; do not treat it as a complete all-in estimate. The provisioning guard requires a complete independently verified receipt including instance, IP, LCU and traffic plus remaining POC budget. No cloud resources have been created by this work.

Sources: https://charts.konghq.com/ ; https://developer.konghq.com/kubernetes-ingress-controller/support/ ; https://www.alibabacloud.com/help/en/cloud-control-api/developer-reference/api-cloudcontrol-2022-08-30-getprice ; https://www.alibabacloud.com/help/zh/slb/classic-load-balancer/product-overview/pay-as-you-go .

Review fixes: the public frontend explicitly rejects user management and environment configuration writes when RAPTOR_PUBLIC_FRONTEND=true, regardless of session role, with normalized-path checks. Kong independently attaches request-termination to those paths/methods. Ordinary environment reads and request operations remain available. Management continues through the private admin service. DNS publication requires independently read owned LB ID/address evidence plus matching no-op LB state; unknown/new addresses are not accepted, so CLB creation and DNS publication are separate stages.

Deferred review minor: kong-system namespace creation is not packaged by this chart. The operator must establish this prerequisite before synchronization; final live bootstrap remains unfinished and no Application has been applied.

Live-plan correction October6: internet CLB ignores vswitch_id, per the official Terraform provider documentation. The public CLB plan therefore has no subnet binding. Account, Singapore zones, exact name and ownership tags identify it; ACK CCM must register only the owned rdev VPC worker, which will be checked after installation. The guard rejects invented subnet bindings. Source: https://registry.terraform.io/providers/aliyun/alicloud/latest/docs/resources/slb_load_balancer .
