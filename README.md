# Raptor Infra Ops Agent POC

This repository starts with a minimal Pi container for later Aliyun Agent Sandbox Gen2 testing in Singapore. It builds only `linux/amd64` and keeps the container running until Pi is explicitly invoked.

See [the agent sandbox image](components/agent-sandbox/README.md) for pinned versions, build instructions and offline acceptance checks.

The image slice is verified: [Actions run 36947281741](https://github.com/jamesDeng/raptor-iap/actions/runs/36947281741) passed the Linux AMD64 build, offline CLI checks and public GHCR publication. The component documentation records the immutable source tag and verified digest. Aliyun template creation, managed sandbox execution, secret-store retrieval, OpenAI calls and infrastructure operations remain separate integration work. No deployment or performance claim follows from the image checks.

Keep credentials outside Git and container images. No credential is required by the offline acceptance checks.

The [AgenticFS bootstrap tool](docs/setup/agenticfs-bootstrap.md) adds locally tested setup/reconciliation and guarded cleanup for storage APIs not covered by the official Terraform provider. Networking and permissions remain Terraform-owned. Actual storage creation, mounting and Pi credential persistence are not yet verified.
