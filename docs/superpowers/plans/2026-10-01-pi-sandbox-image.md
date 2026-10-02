# Pi Sandbox Image Implementation Plan

**Status (2026-10-02):** Image build/offline verification/publication slice complete. [Run 36844666101](https://github.com/jamesDeng/raptor-iap/actions/runs/36844666101) passed build/offline checks and published the tested Linux AMD64 image with verified digest. Authenticated metadata reported public visibility; the publication job and overall workflow failed the approved private-visibility gate. The user explicitly chose to keep the image public on October 2, superseding the private assumption. Both jobs passed in [run 36947281741](https://github.com/jamesDeng/raptor-iap/actions/runs/36947281741), including registry digest, Linux AMD64 configuration and public visibility verification. See [component evidence](../../../components/agent-sandbox/README.md#verified-public-release--2026-10-02). No Aliyun or model integration claim follows.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish a minimal, offline-tested Pi container image for a subsequent Aliyun Gen2 sandbox trial.

**Architecture:** Build a Linux AMD64 image in GitHub Actions, test the installed harness without network access, and publish to GHCR only after success. Keep cloud setup and model calls outside this slice.

**Tech Stack:** Docker, Node.js 22 Debian Bookworm, npm, Pi 0.99.2, GitHub Actions, GHCR.

**Spec:** [Approved design](../specs/2026-10-01-pi-sandbox-image-design.md), approved by the user October 1.

## Global Constraints

- Image architecture: `linux/amd64`.
- Pi: `@earendil-works/pi-coding-agent@0.99.2`; Node engine: `>=22.19.0`.
- Base: official Node.js 22 Debian Bookworm image, pinned by digest.
- Provide `/bin/bash`, Git, curl, CA certificates and writable standard account files.
- Default command: `sleep infinity`; invoke Pi explicitly later.
- Disable npm dependency lifecycle scripts during installation.
- GHCR source image: `ghcr.io/jamesdeng/raptor-iap-pi:sha-<full-commit-sha>`; record published digest.
- Explicit Actions permissions: `contents: read`, `packages: write`; PR jobs cannot publish.
- No OpenAI key, PAT or Aliyun credential in the image/build/tests. No cloud resources or model calls.
- Keep existing POC budgets: below RMB 1,000 Aliyun total; separate USD 5 initial model tests.

## Review Focus

- Fork and same-repository PRs: image checks run, publication does not.
- Old Node or incompatible architecture: integration checks fail before publication.
- Missing utilities or broken Pi install: offline startup checks fail before publication.
- Failed tests or interrupted jobs: test containers are cleaned up; publication depends on successful checks.
- Rerunning a source revision: avoid replacing its image tag with different content.

## Task 1: Container and offline acceptance checks

**Files:**
- Create `components/agent-sandbox/Dockerfile`.
- Create `components/agent-sandbox/.dockerignore`.
- Create `components/agent-sandbox/check-image.sh`.
- Create `components/agent-sandbox/README.md`.
- Create root `.gitignore` and `README.md`.

**Interfaces:**
- Produces an image whose default process stays running and whose `pi --version` is `0.99.2`.
- Produces `check-image.sh IMAGE_REFERENCE`, run on a Docker host; exits zero only when all offline checks pass, otherwise nonzero. Cleans up its test container on success and failure.

- [ ] Resolve the official `node:22-bookworm` image digest from registry metadata; record it and its supported AMD64 variant. Resolve the Pi package tarball/lock metadata to confirm the pinned package has the documented dependency lock data. Stop for a design correction if that premise is false.
- [ ] Write `check-image.sh` using real container checks: network disabled; assert `pi --version` equals `0.99.2`; validate Node >=22.19.0; require `/bin/bash`, Git, curl and a nonempty `/etc/ssl/certs/ca-certificates.crt`; verify `/etc/passwd` and `/etc/group` are writable. Start the default container, confirm it remains running after a short wait, and remove it through an EXIT trap. Do not request or print credentials.
- [ ] Implement the Dockerfile with the resolved digest, utilities and pinned Pi installation using `--ignore-scripts`. Use `/workspace` as the working directory and `sleep infinity` as its default command. Add `org.opencontainers.image.source=https://github.com/jamesDeng/raptor-iap`.
- [ ] Limit the Docker context through `.dockerignore`; exclude `.env*`, credentials and `.pi/` session data in the root `.gitignore`. Write the root/component documentation with exact versions and acceptance boundaries.
- [ ] Check shell syntax locally. Real container checks run in Actions in Task 2 because Docker is absent on the Mac; do not report them as passed before that run.
- [ ] Commit the container/check/documentation files. Review the staged content for accidental credential material.

## Task 2: Build, verify and publish through Actions

**Files:**
- Create `.github/workflows/pi-sandbox-image.yml`.
- Update `components/agent-sandbox/README.md` with actual build evidence after success.

**Interfaces:**
- Consumes Task 1's Dockerfile and `check-image.sh IMAGE_REFERENCE`.
- Produces a successful Actions run URL, source commit, GHCR tag and image digest. These are image-release evidence only.

- [ ] Resolve official checkout, Docker setup/build and registry-login Actions to full commit SHAs; annotate their release versions. Use `ubuntu-24.04` and a 20-minute job timeout. Use explicit Bash steps and disable shell tracing around authenticated operations.
- [ ] Configure `workflow_dispatch`, `push` to `main`, and `pull_request` targeting `main`. Filter automatic runs to `components/agent-sandbox/**` and the workflow file. Build/load the AMD64 image, then invoke Task 1's checks. Keep runtime tests in a job with `contents: read`; grant `packages: write` only to the main-branch publish job.
- [ ] Make publication a separate job dependent on successful checks, gated to a trusted `main` push/manual dispatch. Transfer the already-tested image as a short-lived Actions artifact so the publish job does not publish an untested rebuild. Authenticate using `GITHUB_TOKEN`, without PAT/cloud secrets.
- [ ] Before push, look up the full-SHA tag. If absent, publish; if its existing digest matches the tested image manifest, reuse; if different, fail without overwriting it. Record tag and digest in the job summary. Require observed public GHCR package visibility, per the October 2 user decision, without mutating visibility.
- [ ] Validate workflow structure and conditions locally. Do not add tests that merely repeat YAML literals; use branch/event cases in review and actual workflow runs for behavioral evidence.
- [ ] Commit and push the implementation to the user-owned repository, using the agreed execution method and applicable worktree/branch workflow. Run the build on `main`, inspect failures and repair within scope until offline tests and publication succeed. If a branch/PR is created, attach it to this chat and use its actual workflow evidence; do not infer main publication from a PR build.
- [ ] Verify the successful Actions run and GHCR image metadata through authenticated APIs. Confirm architecture, tag and digest. Report any access error separately from a build error.
- [ ] Record the run URL and digest in component documentation, commit the evidence update, and report that Aliyun template creation, Gen2 launch, secret retrieval and OpenAI calls remain untested.

## Execution and limits

The user previously requested parallel-agent capability during implementation. These two tasks are sequential: Task 2 depends on Task 1's tested image interface. Use that preference where independent implementation/review work is available, rather than running dependent mutations concurrently. The selected implementation skill determines review gates; this plan does not require a new user-owned chat.

This plan ends at image publication. The verified PAT stays in Apple Passwords and is not needed for these Actions jobs. A later Aliyun template build will need another hidden local prompt; its setup is not a claim that credential retrieval is already automated.
