# Foundation execution ledger

2026-10-05: Approved native execution. Branch feat/rdev-foundation based on 5d4d9b4. Native worktree registration unavailable because the calling project root is not a Git repo; reused established ignored manual-worktree location. PR5/6 were open at branch creation; subsequently explicitly authorized and merged.

Ruling: prepare foundation from PR6 but block release apply until source integration is resolved. This preserves public GitOps desired state and avoids silently deploying unmerged platform code.

Verification: five offline preflight tests pass; Raptor HTTP adapter and MCP baseline tests pass with loopback enabled. Foundation provider validation passes and mocked contract passes (one run). Provider desired_size is a string; contract corrected after inspecting verbose mock output. Four-core worker in stock in Singapore a/b/c/d at check time.

Ruling: gateway main currently has only simulation worker wiring; do not advertise live Pi agent execution from cloud deployment. The later runtime integration task must wire the real adapter before claiming end-to-end agent acceptance.

Remaining: complete price/permission evidence and remote state; live ACK version check; refresh integrated source; Helm/Argo CD manifests and image build verification; cloud apply and live acceptance. No cloud resources created.

2026-10-05 integration receipt: GitHub independently confirmed PR5 merge b90785cc64d1e960e92322d723c3bb161ba7b0b2 and PR6 merge 5ea09b21e224bf4cd3fc9d7fdcb3240ce08dd92c after fresh Go race/vet, 25 harness tests and 114 Python tests. Local origin/main remains stale; do not claim foundation branch incorporates the merge yet.

2026-10-05 prerequisite check: RAM GetRole confirmed EntityNotExist.Role for AliyunCSDefaultRole. ACK version discovery remains blocked by this prerequisite. Official ACK roles documentation identifies default and managed-cluster roles plus networking/storage add-on roles. These are cloud service permissions, separate from agent runtime credentials. No role or cluster was created by this check.

2026-10-05 authorized prerequisite result: created AliyunCSDefaultRole and AliyunCSManagedKubernetesRole with official cs.aliyuncs.com trust and corresponding System policies. Independently verified both policy attachments. Read-only creatable version discovery succeeds in Singapore: 1.36.2-aliyun.1, 1.35.7-aliyun.1, 1.34.10-aliyun.1; all list Flannel support. No cluster created.

Integration: fetched origin/main at 5ea09b21e224bf4cd3fc9d7fdcb3240ce08dd92c and merged into foundation branch without conflicts.

Authorization boundary: automatic approval review rejected the initial six-role batch; narrowing to two verified core roles succeeded. After confirming official role definitions and all four missing roles, review still rejected networking/CSI batch because exact permission scope was not explicitly approved. No networking/CSI roles created. Pending exact authorization: AliyunCSManagedNetworkRole, AliyunCSManagedCsiRole, AliyunCSManagedCsiProvisionerRole, AliyunCSManagedCsiPluginRole and their corresponding System policies. These service roles are account prerequisites and must not be destroyed with the POC environment.

2026-10-05 exact authorization completed: user explicitly approved the four named networking/CSI roles and corresponding System policies. Created all four and independently verified each policy attachment. No optional logging/ARMS/diagnostic/autoscaling roles enabled. Missing RDS role independently confirmed: AliyunServiceRoleForRdsPgsqlOnEcs. No RDS role created; exact security authorization pending.

2026-10-05 PostgreSQL prerequisite completed: user approved AliyunServiceRoleForRdsPgsqlOnEcs. Created through RDS CreateServiceLinkedRole in Singapore; independently verified its name and AliyunServiceRolePolicyForRdsPgsqlOnEcs attachment. Official PostgreSQL service name is pgsql-onecs.rds.aliyuncs.com, distinct from MySQL.

Task 3 preparation: added build-only platform image workflow and offline packaging/fail-closed smoke script. No image publication/deployment in this workflow. Local shell syntax, whitespace and five preflight tests pass. Docker is unavailable locally; container smoke checks remain unverified until CI.

Ruling: prepare image validation while pricing/remote state blocks apply — independent source work reduces the wait — cost if wrong: CI packaging may require revision, with no cloud spending.

Verification: all five Go service binaries compile with GOOS=linux GOARCH=amd64 CGO_ENABLED=0. This verifies compilation, not container/runtime acceptance. Added read-only Terraform account-role guard (no role creation/deletion in environment state). New missing-role contract failed before implementation and passes afterward; terraform validate passes and both mocked contract runs pass. Cloud apply remains blocked by complete cost evidence and remote-state/workflow setup.

Task 2 preparation: added separate protected remote-state bootstrap module (private OSS ACL, AES256, versioning, prevent_destroy, Capacity Tablestore LockID table without TTL) and partial encrypted OSS backend declaration for rdev.ali. Bootstrap storage has not been provisioned. Account guard precedes bootstrap resources. State contract failed before implementation and passes afterward; provider validation passes. Corrected test lock-instance name to provider's 16-byte limit. Official signed provider checksums now include Darwin ARM64 and Linux AMD64 in both modules and environment. Environment init with backend disabled and validation pass; both foundation contracts pass. Added credential-free foundation CI; it deliberately has no cloud apply job yet.

Cost quote: ECS DescribePrice for 20 GiB cloud_essd PL1 disk returned CNY0.03344/hour, or 2.40768/72h. Fixed forecast is approximately CNY115.20; usage/storage/locking assumptions and current cumulative billing remain incomplete. Preserve prices_complete=false until resolved.

Ruling: remote-state bootstrap is separate and protected from environment teardown — avoid losing the state needed to reconcile partial provisioning — cost if wrong: small ongoing state storage fees require explicit later cleanup after backing up state.

Planning identity preparation: proposed GitHub OIDC trust with exact repo/environment subject, paired with a read-only cloud metadata policy. New contract failed before implementation; validates and passes after deterministic mock ARN correction. Actual identity/environment configuration requires security approval and has not occurred. Account-wide read visibility is documented explicitly; no state object reads, state lock writes or infrastructure mutations included.

Ruling: establish a read-only planning identity before an apply identity — obtain real provider-plan evidence without granting premature cloud write permissions — cost if wrong: one additional role and a separate later apply-permission review. Forecast scenario is 144.76 CNY but storage/locking unit-price verification is pending; prices_complete remains false.

2026-10-05 planning authorization receipt: user approved the named role/provider and main-only environment. Verified live GitHub issuer certificate chain/hostname and obtained current CA fingerprint ab9d0263244dd0326eb67015705a667e79cfe998 (three certificates). Created provider and read-only role/policy; independently verified issuer/audience/exact subject, sole attached Custom policy and its exact allowed read actions. Aliyun normalized Federated principal to a one-element list; semantic verification passes, with no trust change needed. Created rdev.ali-plan GitHub environment and independently verified main is its only allowed branch. Configured and read back role/provider ARN environment variables; no static credential stored. Added manual main-only OIDC read-only check. Actual STS exchange remains untested until workflow is integrated into main and dispatched. No infrastructure resources created.

### Planning permission review — 2026-10-05

Fresh review identified credential retrieval within ACK Describe wildcards. Treated as an important mismatch with metadata-only intent and resolved with four explicit denies (both kubeconfig APIs, attach scripts, trigger details). Existing grants were unchanged; the live default custom policy was read back and verified. No kubeconfig was retrieved. GitHub OIDC exchange still awaits the main-only verification workflow; no ACK/RDS foundation has been provisioned.
