# Model providers acceptance

Date: 2026-10-10. This receipt covers local code and synthetic fixtures only. The feature flag remains off in Helm defaults.

## Verified locally

- Raptor `go test ./...` passed with disposable PostgreSQL 17 over the local Unix socket.
- Gateway `go test ./...` passed with the same disposable PostgreSQL service.
- Agent harness `node --test *.test.mjs` passed, 28 tests.
- `node --test *.test.mjs` in `components/raptor/web/poc` passed, 29 tests, before the final credential-security review; the Request UI was unchanged by that review.
- Python platform release tests passed, 14 tests, before the final credential-security review; the Helm templates were unchanged by that review.
- Focused AX bridge tests passed with `GO111MODULE=off go test bridge.go bridge_test.go -count=1`. The full pinned AX bridge test script could not fetch its upstream dependency because the local network proxy was unavailable.
- A synthetic Pi OAuth fixture verified device-code challenge, polling, cancellation, and expiry. It did not contact OpenAI or use a real account.

## Deployment gates

The backend, Gateway, Pi harness, and AX guest image must all be built from compatible revisions before enabling `modelProviders.enabled`. The backend credential key must be supplied as a Kubernetes Secret reference, not checked into Git or Terraform state. The backend currently runs as one replica; pending device-code sessions are process local and expire on restart. The database credential is encrypted with a separate file key, but the backend's `raptor_app` database role can read ciphertext. No other deployed service uses that role in the current chart.

## Rdev code rollout

On 2026-10-10, feature PR #99 and GitOps release PR #100 merged. PR #99's four CI workflows passed; the release PR's Platform images workflow passed. Main-branch publication built and pulled the immutable Raptor image `sha256:757f0326abc4eca41cdb7a9acd4f90f8be9ed269c3d91bba9ed71a6e0a8e4ff8` and Gateway image `sha256:5d533744623c01c9850ca3b65703dc6383d138d12309e2187ae44b883593417c` from source `a0964bcd0990afe7d339300ea6826b60952e16af`.

After the release merge, a read-only `crictl` inspection on the owned rdev worker showed the running backend, admin, frontend, and open-api containers using the Raptor image ID mapped to that digest, and the running Gateway container using the Gateway image ID mapped to its digest. The external frontend returned HTTP 200 and served the new `model-help` picker markup and JavaScript. Anonymous `GET /api/v1/model-providers` returned 404 with the flag off; admin model-provider access through the public frontend returned 403; anonymous Request listing returned 401. These checks establish the code rollout and closed public routes, not Argo Synced/Healthy status or authenticated model behavior.

No model-provider migration or encryption-key Secret setup was performed as part of this rollout; their live state was not independently checked. No real Codex sign-in or selected-model Request was run. The private Kubernetes API was unreachable from this laptop, so direct Argo and database verification were unavailable. Private hosted use of one person's ChatGPT-plan credential remains an unverified account eligibility gate; see [qualification](model-provider-auth-qualification.md). Keep the feature flag off until the remaining gates pass.
