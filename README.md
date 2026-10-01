# Raptor Infra Ops Agent POC

This repository starts with a minimal Pi container for later Aliyun Agent Sandbox Gen2 testing in Singapore. It builds only `linux/amd64` and keeps the container running until Pi is explicitly invoked.

See [the agent sandbox image](components/agent-sandbox/README.md) for pinned versions, build instructions and offline acceptance checks.

This slice establishes image construction and offline CLI startup once the Docker checks pass. Aliyun template creation, managed sandbox execution, secret-store retrieval, OpenAI calls and infrastructure operations remain separate integration work. No deployment or performance claim follows from the image checks.

Keep credentials outside Git and container images. No credential is required by the offline acceptance checks.
