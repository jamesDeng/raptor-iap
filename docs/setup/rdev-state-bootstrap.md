# Protected rdev state bootstrap

Prepared configuration, not provisioned: `infra-terraform/environments/rdev-state-bootstrap`. This root is independent of rdev.ali and uses local state only during initial bootstrap. Run with private inputs and a private TF_DATA_DIR/local state path, never a state file in the public checkout. Review a saved plan before applying. After creation, migrate bootstrap state to its own OSS key and preserve a restricted backup. Import existing planning identities into a separate protected key before managing them with Terraform.

Resources: one private Standard OSS bucket, AES256, versioning, nonempty deletion disabled; one Singapore HighPerformance Tablestore instance; one `terraform_lock` table with String `LockID`, TTL -1 and one version. Terraform prevent_destroy protects bucket/instance/table. It is not a cloud permission boundary. Verify encryption/ACL/versioning and zero reserved read/write throughput after creation. No creation has occurred.

Use a globally unique bucket name supplied privately and a 3–16 character lock instance name. Use the Singapore public OTS endpoint for GitHub runners. rdev.ali's backend config contains bucket, endpoint, table, and explicit prefix `rdev.ali`, with key `terraform.tfstate`; its effective state object path must be confirmed against Terraform 1.13.3 before constraining permissions. Bootstrap and identity state keys must be excluded from the environment deploy role.

Credential and permission separation:

| Identity | Intended access | Boundary |
| --- | --- | --- |
| Local bootstrap operator | Create/configure only the named OSS bucket and OTS instance/table; import existing identities | One-time reviewed plan; no bucket/table deletion; current account guard |
| Planning GitHub role | Metadata reads, plus eventual state read and lock-row access | Existing `rdev.ali-plan` environment, main only; no workload mutation |
| Deployment GitHub role (not created) | Reviewed foundation resource mutations and rdev state write/locks | Separate `rdev.ali-apply` environment with reviewer protection; exact immutable repo subject; no general RAM administration |

State backend candidate actions to verify before attachment: OSS GetBucketLocation/ListObjects/GetObject/PutObject, scoped to the dedicated bucket and effective rdev state path; OTS DescribeTable/GetRow/PutRow/DeleteRow/UpdateRow, scoped to the exact instance/table. DeleteRow removes a lock and is required for normal release; it does not grant DeleteTable/DeleteInstance. Backend read-only planning still acquires locks and is therefore not purely read-only storage access. Do not attach an account-wide OSS/OTS administration policy.

Cloud apply policy is not complete or installed. Enumerate the pinned provider's create/update/read API calls for ACK/nodepool, VPC/vSwitch/NAT/EIP/SNAT, RDS and required role passing. Resource-create calls may need resource wildcards before IDs exist; use documented tag/region constraints only where the API supports them, then narrow known IDs. Do not claim all create permissions can be scoped to an unknown future resource ID. Exclude unrelated existing sandbox, FC, DNS/certificates and checkpoints. Teardown is a separately reviewed permission/plan.

Sources: https://developer.hashicorp.com/terraform/language/backend/oss (read the 1.13 version and source before live backend use); https://help.aliyun.com/en/tablestore/authorization-policy-syntax-and-elements ; https://help.aliyun.com/zh/oss/developer-reference/put-bucket-encryption .
