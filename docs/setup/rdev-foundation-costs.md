# rdev.ali 72-hour cost worksheet

Status: incomplete preflight, not authorization to apply. Quotes are not a billing cap. Currency CNY, Singapore, checked 2026-10-05. Whole-POC cumulative spending must remain below 1000; foundation allocation 150.

| Item | Quantity and quoted rate | 72h estimate | Evidence |
| --- | --- | ---: | --- |
| ECS worker + 40 GiB ESSD | one ecs.e-c1m2.xlarge, 0.80438/h | 57.92 | account ECS DescribePrice |
| PostgreSQL Basic + 10 GB high-performance storage | pg.n2e.1c.1m, 0.088/h | 6.34 | selected RDS console quote |
| CLB base | two, 0.147/h each | 21.17 | official CLB pricing |
| Public application CLB IP | one, 0.04/h | 2.88 | Singapore CLB pricing |
| Enhanced NAT base | one, conservative cross-zone 0.30/h | 21.60 | official NAT pricing |
| NAT EIP holding | one, 0.04/h | 2.88 | prior Singapore EIP quote |
| Prometheus 20 GiB ESSD PL1 | one, 0.03344/h | 2.41 | account ECS DescribePrice disk response |
| Fixed subtotal | | **115.20** | rounded line totals |

Usage-dependent costs still need final assumptions and quotes: NAT processing (Singapore cross-zone 0.30 per GiB inbound + outbound), CLB LCUs, public outbound traffic, FC/API Gateway execution, limited sandbox runtime, encrypted OSS/checkpoint storage and Tablestore state locks. Hour-boundary rounding and provisioning time require headroom. State storage persists beyond the 72h window and is not automatically deleted with the environment. No reserved Tablestore throughput or search indexes are planned.

Sources:
- https://help.aliyun.com/zh/slb/classic-load-balancer/product-overview/pay-as-you-go
- https://help.aliyun.com/zh/nat-gateway/nat-gateway-billing
- https://help.aliyun.com/en/api-gateway/traditional-api-gateway/product-overview/serverless-instance-1
- https://help.aliyun.com/en/functioncompute/pay-as-you-go-billing-methods
- https://help.aliyun.com/en/agent-sandbox/product-overview/billing-overview
- https://help.aliyun.com/en/tablestore/product-overview/billable-items-and-billing-methods/

Do not set prices_complete=true based on the fixed subtotal. Refresh account balance/billed and unbilled costs before apply. Preserve sanitized quotes without credentials; never commit raw CLI configuration or billing exports.

## Bounded test scenario (forecast assumptions, not verified usage)

Provisioning candidate: fixed 115.20 + CLB 1 LCU per instance-hour (2 × 72 × 0.049 = 7.06) + combined public egress 10 GB (7.50 at 0.75/GB) + NAT total processed traffic 20 GiB (6.00 at 0.30/GiB) + sandbox runtime allowance 4.00 + FC/API calls allowance 2.00 + OSS/checkpoint/Tablestore allowance 1.00 + hour-rounding allowance 2.00 = **144.76**, leaving approximately **5.24** headroom. These usage assumptions are not enforced cloud limits. The storage/locking allowance needs regional price verification; do not mark the cost gate complete yet. Avoid load testing, prolonged sandbox sessions or repeated image downloads in this initial window.

Billing check: China-site billing endpoint business.aliyuncs.com with region cn-hangzhou reported available balance 986.00 CNY and October billed total 14.00 CNY (domain). Delayed/unbilled usage may not appear yet. Initial calls using the Singapore billing endpoint failed due to site mismatch; corrected endpoint succeeded.

## Singapore state-store price verification (2026-10-05)

Official regional pricing, selected Singapore in the rendered tables:
- Tablestore Capacity: unsupported; use HighPerformance, zero reserved read/write throughput. Storage 0.0018 CNY/GB/hour; on-demand reads 0.02/10,000 CU; writes 0.04/10,000 CU; public egress 0.75/GB. Source: https://cn.aliyun.com/price/detail/ots?from_alibabacloud= .
- OSS Standard LRS storage: 0.136 CNY/GB/month above the published free tier; requests 0.01/10,000 above the published free tiers; egress 0.75/GB at the first paid tier. Source: https://cn.aliyun.com/price/detail/oss?from_alibabacloud= . Do not assume account free allowances remain available.

Conservative state/checkpoint scenario ignoring free allowances: combined OSS retained versions/checkpoints 1 GiB (72h storage 0.0136), 10,000 GET + 10,000 PUT-class requests (0.02), combined OSS/OTS public egress 1 GiB (0.75), OTS 0.01 GiB retained data (0.001296), 10,000 read CU + 10,000 write CU (0.06). Total approximately 0.85 CNY; retain the existing 1.00 allowance. This keeps the full forecast at 144.76 CNY. Larger checkpoints, retries and public downloads can exceed these assumptions; they are usage estimates, not enforced caps.

Persistent state cost after the test: OSS 1 GiB without allowance is approximately 0.136 CNY/month; OTS 0.01 GiB approximately 0.01296 CNY/30-day month, plus requests/egress. The retained bucket includes old versions in this volume estimate. No automatic state deletion. The pinned provider creates the table with a zero-valued ReservedThroughput structure; verify actual zero settings after creation before using it.

Latest account balance refresh: 985.99 CNY. Previous billed domain cost was 14 CNY; the balance change does not attribute all delayed/unbilled costs. Storage regional rates are now verified, but prices_complete remains false until FC/API and sandbox allowances are backed by explicit quantities and the account's current usage is reconciled.
