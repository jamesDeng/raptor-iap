# Dedicated test PostgreSQL module

Owns one private pay-as-you-go PostgreSQL RDS with explicit SKU/version/storage/zone, bounded private client CIDRs and `env`/`db-code` discovery tags. It creates no accounts/passwords, databases, public endpoint or shared network resources. Credentials and schema initialization belong to a separately reviewed central path. Deletion protection is enabled; teardown needs its own concrete review.

Provider/tool versions match foundation pins. Both plan-time and deferred apply-time account guards precede cloud resources. Caller must bind the provider to Singapore and validate every supplied subnet/security input against that account/VPC. Tests use mocked providers only and establish configuration contracts, not SKU availability or provisioning success.
