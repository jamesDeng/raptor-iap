# Dedicated PgCat traffic baseline

Bootstrap namespace and project, then privately provision `traffic-db-dsn` and the provider-derived `pgcat-provider-inventory` ConfigMap before creating the two Argo Applications. The client uses the private NLB and restricted `poc_app` account. Plaintext is an explicit private-network POC decision. No credentials or provider addresses are committed here. Prometheus has no Ingress.

The headless Service and DNS scrape configuration must be enabled together to observe both replicas. Refresh inventory from ESS/ECS after membership changes and before any replacement acceptance test. This baseline snapshot does not provide automatic inventory reconciliation; stale or missing nodes must not be treated as zero connected clients. Prometheus emptyDir retention is two hours and is not a durable audit archive.
