# Pi sandbox image and build workflow

Status: written design awaiting user review. No image, workflow or sandbox has been built.

## Intent and established decisions

Deliver the first buildable component of the Infra Ops Agent POC: a minimal Pi image and a GitHub Actions workflow that publishes it to GHCR for later testing in Aliyun Agent Sandbox Gen2 in Singapore. This slice proves image construction and offline harness startup; it does not prove managed-sandbox or model integration.

Direct user decisions in the current conversation: one `jamesDeng/raptor-iap` repository, Pi as the harness, Aliyun Singapore, GitHub Actions, OpenAI API for unattended runs, credentials outside Git/images, and Apple Passwords for the dedicated GHCR token. Total Aliyun POC spend remains below RMB 1,000; initial model tests have a separate USD 5 budget.

The Actions/GHCR image path was proposed by the assistant and accepted by the user's subsequent “no problem, go ahead.” The image choices below are implementation recommendations for review.

## Approach

Use GitHub Actions to build on an AMD64 runner and publish to GHCR. This avoids requiring Docker on the local Mac. A paid ACR EE instance adds unnecessary setup for this slice. Installing Pi into an official Aliyun image at each sandbox launch would be a useful disposable probe, but does not deliver the reusable custom image selected here.

The image uses the official Node.js 22 Debian Bookworm image, with its exact digest resolved and recorded during implementation. It must provide Node >=22.19.0, `/bin/bash`, Git, curl and CA certificates. Install the exact npm package `@earendil-works/pi-coding-agent@0.99.2` with dependency lifecycle scripts disabled. Use the package's supplied dependency lock data; record the resolved base digest and Pi version in the image documentation.

Build only `linux/amd64`, as required by Aliyun. Keep the container alive with `sleep infinity`; Pi is invoked explicitly through sandbox command execution in the subsequent integration slice. Preserve standard writable `/etc/passwd` and `/etc/group` for Aliyun initialization. Do not add gateway, Infra API, Terraform tooling, application skills or model configuration to this first image.

## Files and responsibilities

- `components/agent-sandbox/Dockerfile`: pinned base, OS utilities, pinned Pi installation and default keep-alive command.
- `components/agent-sandbox/.dockerignore`: limit the image context to its intended build inputs.
- `components/agent-sandbox/README.md`: build/run instructions, pinned versions, acceptance evidence and the boundary between a working container and an untested managed sandbox.
- `.github/workflows/pi-sandbox-image.yml`: build, offline test and conditional publication.
- Root `.gitignore`: exclude local environment files, credentials and Pi session state.
- Root `README.md`: describe this repository's initial scope and link to the component documentation.

## Workflow and publication

Run on manual dispatch and changes to the image/workflow on `main`; pull requests run build/tests without publication. Publish only from the trusted `main` branch after tests pass. Use the workflow's built-in `GITHUB_TOKEN`, with explicit `contents: read` and `packages: write`; no Aliyun credential or personal token is needed in Actions for this slice. Pin external Actions by full commit SHA, with the corresponding release noted.

Publish `ghcr.io/jamesdeng/raptor-iap-pi:sha-<full-commit-sha>`. Do not overwrite a published source tag with different content; an identical rerun may reuse it, and a different result must use a distinct tag. Record the pushed digest in the job summary. Keep the package's initial private visibility; the dedicated GHCR token will provide Aliyun pull/push access later. The public source repository does not imply public package visibility.

## Credential boundary

No OpenAI key, GitHub PAT, Aliyun credential or local password enters the Docker build, image or test logs. The already verified GHCR PAT remains in Apple Passwords. Aliyun's default custom-image processing needs pull and push rights to the source registry; supplying that token to Aliyun belongs to the later template-build step via a hidden local prompt. Its use here is not required.

The final platform requirement remains Pi retrieving its OpenAI key from a secret store at runtime and calling OpenAI directly. This image does not substitute runtime environment injection for the unimplemented secret-store integration.

## Acceptance and failure behavior

The Actions job must build the AMD64 image and run a container with networking disabled to verify: `pi --version` reports `0.99.2`; Node meets the version floor; Bash, Git, curl and certificate files are present; and the default keep-alive container remains running until explicitly removed. These checks require no API credential and cannot make a model call.

Run cleanup even when a check fails. A failed build or offline check prevents publication. A publication failure fails the job and must not be reported as a completed image release. A successful release has a job URL, source commit, image tag and digest; it establishes neither Aliyun reachability nor Gen2 execution.

No cloud resource, template or model call is created by this slice. Next, separately verify the actual GHCR-to-Aliyun template build and Gen2 launch, then Pi/model/secret retrieval. Private VPC access and platform orchestration remain later work.

## Evidence

- Current GitHub API checks: authenticated user has push/admin access; Actions enabled; default workflow permissions are read-only.
- Local hidden-prompt verification: expected account `jamesDeng`, `write:packages`, no `repo` scope; registry push not tested.
- October 1 npm metadata: current official Pi package `@earendil-works/pi-coding-agent` is `0.99.2`, Node engine `>=22.19.0`; the earlier `@mariozechner` package is a different, older release.
- [Pi official installation documentation](https://github.com/earendil-works/pi/tree/main/packages/coding-agent).
- [Aliyun custom image requirements and registry handling](https://help.aliyun.com/zh/agent-sandbox/build-a-custom-image-template).
- [GitHub Container Registry authentication and package visibility](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).
