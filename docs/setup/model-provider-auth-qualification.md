# Codex model-provider authentication qualification

Date: 2026-10-10. This is a synthetic, credential-free qualification for the planned Raptor Admin connection. It is not live account authorization or deployment evidence.

## Result

- `device_code_supported=true`: the pinned `@earendil-works/pi-coding-agent` 0.99.2 public `ModelRuntime.login(providerId, type, interaction)` API accepts `providerId="openai-codex"`, `type="oauth"`, and an interaction that selects `device_code`. It emits a `device_code` event, polls, returns an OAuth credential on success, and honors cancellation/expiry. The model catalog for this Pi provider includes `gpt-5.6-luna` and other Codex models. The separate Pi provider `openai` invokes a different Sign in with ChatGPT path.
- `private_hosted_shared_account_eligible=false` as a rollout gate: eligibility is **unverified**, not disproved. [OpenAI's Sign in with ChatGPT overview](https://developers.openai.com/siwc/token-sharing-open-source) describes open-source/local usage and directs remotely hosted applications to an interest form. Its [quickstart](https://developers.openai.com/siwc/quickstart) mentions selected private clients. Neither source establishes that Raptor may use one person's ChatGPT plan for all users of this hosted internal platform. These sources describe the newer Sign in with ChatGPT integration; they do not document a permission grant for Pi's legacy `openai-codex` flow in this deployment.
- `live_activation_supported=false` until that account/use eligibility is confirmed and a real device authorization plus selected-model Request passes separately.

## Synthetic proof

Run from `components/agent-harness` after `npm ci --ignore-scripts --no-audit --no-fund`:

```sh
node --test model-auth-qualification.test.mjs
```

On 2026-10-10, this returned 3 tests passed, 0 failed: device-code prompt/poll success, cancellation without stored credential, and expiry without stored credential. The fixture intercepts auth requests, uses an invented account claim/token, and makes no network request. No personal credential was read, printed, or saved.

## Activation rule

Build and test the provider management feature behind its off-by-default flag. Do not run a live admin sign-in, enable the model for users, or claim acceptance from the synthetic fixture until supported private hosted shared-account use is confirmed. If eligibility is unavailable, keep the Codex connection disabled and retain the generic provider/model framework for a future organization-owned provider.
