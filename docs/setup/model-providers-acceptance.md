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

No production sign-in, live model Request, image build, PR CI, merge, or deployment is established by this receipt. Private hosted use of one person's ChatGPT-plan credential remains an unverified account eligibility gate; see [qualification](model-provider-auth-qualification.md). Keep the feature flag off until the remaining gates pass.
