# Environment storage migration

## Source inventory

This inventory is from the tracked repository. It does not claim to enumerate values in a deployed database. The migration iterates every key in every existing `config` object, so unrecognized and nonstring values remain in `environment_settings.value_json` as JSON text.

| Existing key | New storage | Readers |
| --- | --- | --- |
| `cloud`, `region`, `accountId`, `ackClusterId` | Named environment columns | Admin, catalog HTTP, request context and MCP; `ackClusterId` also in request validation and agent access |
| `cloudAccountId` | `account_id` plus preserved legacy key | rdev platform bootstrap and infra discovery guard |
| `clusterId`, `namespace`, `infraApiUrl` | Named environment columns | Infra adapter; `namespace` also in agent MCP projection |
| `terraformRepo`, `terraformBaseBranch`, `terraformPath` | Terraform row in `environment_repositories` | Existing HTTP/MCP `config` response; Admin form |
| `kubernetesRepo`, `kubernetesBaseBranch`, `kubernetesPath` | Kubernetes row in `environment_repositories` | Existing HTTP/MCP `config` response; Admin form |
| `k8sRepo`, `k8sBaseBranch`, `k8sPath` | Kubernetes row plus preserved legacy keys | rdev platform bootstrap and compatibility response |
| Any other key, any nonstring value, and empty string values | `environment_settings` keyed by environment and original key | Existing HTTP/MCP `config` response |

The repository has no other production direct SQL reader of `environments.config` after `agentaccess` switches to `ack_cluster_id`. `catalog` assembles the compatibility response from columns and related records. `requests`, `adapters`, `openapi`, and `agentaccess` continue to see their prior values. The agent MCP filter continues to expose only `ackClusterId`, `region`, and `namespace`.

An environment may have any number of repository rows, including several rows with one purpose. The `repositories` HTTP field contains them all. Legacy `config.terraformRepo` and `config.kubernetesRepo` expose the first row of each purpose in stable row-ID order for older consumers. A URL may appear in both purposes.

## Rollout checks

Before applying to an environment, inspect actual keys and types with a read-only query:

```sql
SELECT key, jsonb_typeof(value) AS value_type, count(*)
FROM raptor.environments CROSS JOIN LATERAL jsonb_each(config)
GROUP BY key, value_type ORDER BY key, value_type;
```

Review any credential-bearing values before rollout. The migration does not discard them or add them to repository URLs. On a copy of the deployment database, run the migration and compare each pre-migration `config` object to the assembled HTTP response, including unknown keys, nested values and empty strings. Confirm the two repository purposes may share one URL, then verify a second migration run is a no-op. Do not run the migration against the live database until this inventory and comparison pass.

The schema migration removes only the `environments.config` column. Other JSONB columns in request, event and approval tables are outside this change.
