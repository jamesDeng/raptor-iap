# Raptor Infra Ops Agent POC

The first connected Raptor slice lets one local owner register application context, submit natural-language questions, and inspect durable task history. A separate gateway runs each task through Pi in an Aliyun Singapore sandbox, retrieves a bound private answer, saves an encrypted OSS credential checkpoint, and confirms compute/control-key cleanup.

Start with [the local Raptor guide](docs/setup/raptor-local.md). Context is manually declared; planned resources are not live infrastructure. This slice has no deployment, restart, live discovery or infrastructure-changing tools. ACK/Argo CD, PostgreSQL RDS, ECS/PgCat replacement and the zero-failed-operations test remain future work.

The [OSS-backed lifecycle guide](docs/setup/oss-sandbox-lifecycle.md) records the separately verified normal → refresh → replacement baseline. The [pinned sandbox image](components/agent-sandbox/README.md) has its own build receipt; that image is not substituted for the proven official Gen2 template by this slice.

[Terraform networking/RAM preparation](docs/setup/sandbox-storage-network.md) and the [AgenticFS bootstrap tool](docs/setup/agenticfs-bootstrap.md) remain separate setup components. AgenticFS approval/mounting is not a prerequisite for the current encrypted OSS checkpoint route.

Credentials, application context, questions and answers stay outside Git and CI. Offline checks use synthetic data and no cloud credentials.
