---
name: repository-change
description: Choose a repository configured for the selected environment and create a reviewable pull request.
---

# Repository change

Read `/workspace/.raptor-repositories.json` before choosing where to work. Every row is attached to this attempt. The `purpose` field tells you when to use it:

- `terraform`: Alibaba Cloud resources and component IAM, including Terraform modules and environment composition.
- `kubernetes`: Kubernetes manifests, Helm values, Argo CD, and runtime configuration tracked in Git.

Use the row whose purpose and directory match the requested change. If several rows fit, inspect their paths and repository contents before selecting one. Do not guess from the environment name or hardcode a repository URL. Use the row's `path` as the starting directory, `baseBranch` as the PR base, and `baseCommit` as the reviewed starting point. If no row fits, explain the ambiguity and stop.

Create a working branch from the pinned checkout. Keep edits inside the chosen repository. Run focused checks, review the diff, commit, push, and open a PR against the configured base branch. Use the private Git and `gh` credentials already installed in the sandbox; never print, copy, or persist the PAT. Do not apply Terraform, change live infrastructure, merge, rerun workflows, or approve your own PR.

Call `pull_request_attach` with this request and attempt, the repository's `owner/name`, `baseCommit`, `baseBranch`, your working branch, the PR number and URL, and the exact head SHA. The request is complete only after that tool confirms attachment. Report the PR link and checks you ran.
