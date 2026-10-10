# Model providers and Request model selection

Status: design approved in chat on 2026-10-10; implementation has not begun.

## Purpose and scope

Raptor managers configure the model providers available to the platform and enable individual models. A user creating an `application.question` Request chooses one enabled provider/model pair. The first connection uses the owner's personal ChatGPT account through Codex to get the platform started. The long-term design accommodates Amazon Bedrock, Alibaba Cloud Bailian (百炼), and the OpenAI API with organization-owned credentials. This is a private internal platform, and one admin-managed connection serves its Requests. No per-user provider accounts are part of this slice.

The first release implements the provider framework, Codex connection, model policy, Request selection, and execution binding. Future provider adapters are interface definitions and extension points, not claims that Bedrock, Bailian, or OpenAI API execution works today. The current fixed `gpt-5.6-luna` path remains in place until the new path passes the rollout gates.

## Verified starting point

- Raptor Admin is a separate UI that proxies `/api/` to the Raptor backend; admin authorization and browser CSRF checks live in the backend (`components/raptor/internal/admin/server.go`, `components/raptor/internal/auth/http.go`).
- The Request form has one hard-coded model option, and Raptor validation, Gateway binding validation, and the Pi harness each require `gpt-5.6-luna` (`components/raptor/web/poc/index.html`, `components/raptor/internal/requests/schema.go`, `components/agent-gateway/internal/execution/live_models.go`, `components/agent-harness/live-contract.mjs`).
- The harness reads Pi OAuth state from sandbox-local `auth.json`. Encrypted checkpoints currently carry that state and conversation sessions; their refresh/crash loss window is documented (`components/agent-harness/pi-adapter.mjs`, `docs/setup/raptor-local.md#checkpoint-limitation-and-budget`).
- The local Pi POC previously used the owner's ChatGPT Pro account. This is evidence of that local run, not evidence that a shared hosted deployment is approved (`docs/setup/raptor-local.md#preparation-and-launch`).

## Architecture

Raptor owns provider connections, encrypted credential records, discovered model metadata, and the manager's enabled-model policy. Gateway owns attempt scheduling and requests a credential for an authorized, bound attempt. The provider adapter turns that credential and model ID into a Pi runtime model. A Request definition records stable `providerId`, `modelId`, and the connection version selected at creation; it never stores a token. Provider-specific behavior stays behind four operations: start/complete connection, discover models, acquire/refresh execution credential, and bind execution model.

The public model-list endpoint returns only enabled, connected models and nonsecret display metadata. Admin endpoints expose provider status, discovered models, and enabled flags but never credential bytes. A catalog entry is not an entitlement guarantee; only a successful inference confirms that the account can use that model. The UI distinguishes discovered, enabled, and last-verified states.

For the first adapter, the internal provider key is `codex` and model IDs are the exact IDs accepted by the pinned Pi runtime. Subsequent adapters may use different credential and discovery mechanisms without changing the Request selection contract. Provider IDs are namespaced in persisted definitions and attempt bindings so identical model names from different providers cannot collide.

## Admin connection and credential lifecycle

Only an authenticated Raptor admin may start, complete, disconnect, or reconfigure a connection and change model policy. Mutations require the existing CSRF protection and an audit row containing actor, action, provider, time, and result without secrets.

The Codex setup UI offers device-code browser authorization: show the verification URL, user code, expiry, and pending/complete/error state. The admin completes authorization in their browser; the server polls using a bounded interval and deadline. A login session is single-use, bound to the initiating admin session and provider, and cannot be resumed from another browser session. The UI never receives access or refresh tokens. Cancel/expiry invalidates the pending session. Existing working credentials remain active until a new sign-in has completed and been validated.

Implement this through a supported provider authorization flow. Do not implement an undocumented token exchange by copying private Pi internals. Before enabling live sign-in, verify the selected device-code path against the pinned client and OpenAI's current private-hosted eligibility. If the path is unsupported or the account is ineligible, expose a clear unavailable state and leave the previous connection intact. Do not treat a manually copied personal token as an equivalent product flow.

Store the issued credential encrypted before writing it to PostgreSQL. The encryption key is supplied by a private runtime secret or managed key service; it does not appear in source, Terraform state, diagnostics, browser responses, or the same database. Persist provider, encrypted payload, key version, credential generation, account display label, expiry, status, and timestamps. Grant only the backend credential service access to ciphertext; keep ordinary Raptor readers and frontend database roles away from that table. Restrict database backups and exports as secret-bearing artifacts.

Refresh is single-writer per connection. A worker uses a short lease or database row lock, refreshes, then commits the replacement only if the credential generation still matches. A losing worker reloads the newer generation. A refresh failure preserves the last encrypted record and marks the connection as needing attention without returning provider error bodies to the browser. Reauthorization replaces the credential atomically. Disconnect disables new and queued attempts and deletes the stored credential after recording a secret-free audit event; remote revocation is attempted when supported and reported separately from local deletion.

The database is the credential source of truth. Existing checkpoints remain the source for per-Request Pi conversation state. For each attempt, Gateway obtains the selected connection's current credential through a service-authenticated, request-bound internal API; it prepares sandbox-local Pi auth state with restrictive permissions and ensures the credential is excluded from public progress, job definitions, model input, logs, and returned artifacts. A refreshed credential is persisted back to the database before the attempt is declared durably complete. Checkpoint publication must not overwrite a newer database credential. If refresh publication is uncertain after a crash, stop subsequent attempts until the connection is reconciled or reauthorized; do not silently reuse an old refresh token.

## Model discovery and policy

The adapter returns model IDs and nonsecret labels/capabilities from the pinned runtime or provider API, with discovery time and source. Refreshing the catalog never automatically enables new models or removes policy history. Managers explicitly enable or disable each model. Unknown IDs cannot be enabled. A disappearing model remains visible to admins as unavailable and is omitted from the user-facing list. A configured default must be enabled and connected; if no model meets that condition, the Request form disables the AI question action with a clear setup message.

Raptor validates the selected provider/model against the current enabled policy at Request creation. It records that exact pair and connection generation in the immutable definition and dispatches it through the outbox. Gateway independently validates the binding and obtains a matching active connection before starting inference; the harness verifies the selected and actual provider/model in its result. No fallback to another model or provider occurs silently. Disabled/disconnected connections block queued and future attempts. Active attempts are asked to cancel on disconnect; their outcome remains explicit if cancellation cannot be confirmed. Completed history keeps the originally requested and actual models.

## Browser and API behavior

Admin: list providers, connect Codex, view connection state, refresh discovered models, enable models, choose a default, and disconnect. Provider cards display account label and status, not token fragments. The login UI resembles the supplied TeamClu reference in behavior, while Raptor stores the credential in its own database rather than Pi's on-machine store.

Request: fetch the enabled list from Raptor on opening the form; show provider and model labels, with the configured default preselected. Show a loading/error state instead of using stale hard-coded options. On submission, send IDs only. A stale choice rejected by the server causes the form to refresh the list and asks the user to choose again. Saved Request and result views show requested provider/model and actual provider/model.

API contracts: `GET /api/v1/model-providers` lists public enabled models for signed-in users; `GET /api/v1/admin/model-providers` lists nonsecret admin status and discovered models; `POST /api/v1/admin/model-providers/codex/connect` starts device authorization; `GET /api/v1/admin/model-providers/codex/connect/{sessionId}` reads its state; `POST /api/v1/admin/model-providers/codex/disconnect` disconnects; `POST /api/v1/admin/model-providers/codex/discover` refreshes the catalog; and `PUT /api/v1/admin/model-providers/{providerId}/models` sets the enabled IDs and default as one versioned policy update. Admin routes use the existing admin and CSRF middleware. Gateway uses private `POST /internal/v1/model-credentials/lease` and `POST /internal/v1/model-credentials/refresh` endpoints with service authentication. Credentials are absent from browser-readable schemas. Service calls bind connection, Request, attempt, model, and expiry; Gateway cannot request an arbitrary provider credential without a valid scheduled attempt. Frontend proxy rules must prevent non-admin access to admin writes even when URLs are guessed.

## Rollout and verification

Use additive Raptor migrations. Deploy backend/provider APIs and Gateway/harness compatibility before enabling the frontend choice. Keep the existing fixed model path behind a feature flag until all components advertise compatible provider bindings. Do not migrate existing Requests' immutable definitions. Treat an existing OSS checkpoint as historical execution state; do not import its credential into PostgreSQL implicitly. A manager signs in through the new flow.

Offline verification covers admin/CSRF denial; one-use, expiring login sessions; encrypted-at-rest records; no token in browser/API/logs/state; concurrent refresh compare-and-swap; discovery versus enablement; stale/disabled model denial at both Raptor and Gateway; no cross-provider or cross-Request credential lease; exact selected/actual model accounting; credential rotation while an attempt is queued; disconnect and active cancellation; checkpoint restoration without credential rollback; and UI loading/error states. Use synthetic credentials and provider fixtures for CI.

Live acceptance is a separate, explicit gate: verify a supported authorization method and account eligibility for this private hosted use, complete admin sign-in, enable one model, submit a real Request, observe the exact model and credential generation in sanitized evidence, confirm a refresh survives a later sandbox, and confirm disconnection blocks new work. Do not describe fixture results as deployed credential or inference proof. No cloud provisioning, deployment, or real credential capture is authorized by this design document alone.

## Alternatives considered

Using the existing encrypted checkpoint as the credential owner with a database pointer would not meet the owner's database-storage requirement and would retain its refresh/crash ambiguity. Replacing Pi with Codex app-server immediately would expand this feature into a runtime and conversation migration. The selected design extends the current Pi path while separating provider policy and credentials from Request data.

## External source limits

OpenAI's [ChatGPT plan usage overview](https://developers.openai.com/siwc/token-sharing-open-source) describes open-source/local usage and directs remotely hosted apps to an interest form. Its [quickstart](https://developers.openai.com/siwc/quickstart) says selected private clients have plan usage. These documents do not establish eligibility for this Raptor deployment or authorize sharing a personal plan across a team. [Codex app-server guidance](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server) says a model list may be bundled and is not an entitlement check. These are current documentation constraints to verify at live acceptance, not assertions about the existing Pi credential's deployment rights.
