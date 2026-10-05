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

Usage-dependent costs still need final assumptions and quotes: NAT processing (Singapore cross-zone 0.30 per GiB inbound + outbound), CLB LCUs, public outbound traffic, FC/API Gateway execution, limited sandbox runtime, encrypted OSS/checkpoint storage and Tablestore state locks. Hour-boundary rounding and provisioning time require headroom. State storage persists beyond the 72h window and is not automatically deleted with the environment. No reserved Tablestore capacity or search indexes are planned.

Sources:
- https://help.aliyun.com/zh/slb/classic-load-balancer/product-overview/pay-as-you-go
- https://help.aliyun.com/zh/nat-gateway/nat-gateway-billing
- https://help.aliyun.com/en/api-gateway/traditional-api-gateway/product-overview/serverless-instance-1
- https://help.aliyun.com/en/functioncompute/pay-as-you-go-billing-methods
- https://help.aliyun.com/en/agent-sandbox/product-overview/billing-overview
- https://help.aliyun.com/en/tablestore/product-overview/billable-items-and-billing-methods/

Do not set prices_complete=true based on the fixed subtotal. Refresh account balance/billed and unbilled costs before apply. Preserve sanitized quotes without credentials; never commit raw CLI configuration or billing exports.
