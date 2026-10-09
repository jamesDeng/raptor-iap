# Raptor ACP bridge for T3 Code

T3 acts as the local UI. This provider submits `application.question` through authenticated Raptor HTTP APIs; Raptor selects the existing Gateway and remote Pi sandbox. No local Pi or model API key is needed. The pinned T3 coding/runtime envelope is removed at exact user-question boundaries; ambiguous envelopes are rejected. Each prompt is a fresh question; transcript replay does not create conversational memory in Pi.

## Build and configure

Requires Go 1.25+. From this directory:

```sh
go build -o /absolute/private/bin/raptor-acp ./cmd/raptor-acp
```

Create a private directory (0700) outside source control. Save `credentials.json` (0600) with an existing Raptor operator's `username` and `password`. Save `config.json` (0600), using absolute paths:

```json
{
  "origin": "https://your-raptor-origin",
  "applicationCode": "your-application",
  "environmentCode": "your-environment",
  "model": "gpt-5.6-luna",
  "credentialsFile": "/absolute/private/credentials.json",
  "stateDir": "/absolute/private/bridge-state"
}
```

Cookies and CSRF tokens stay in process memory. Session state contains prompts, answers and request mappings; keep it private. One session bridge process owns each state directory; this POC supports one open T3 thread per configured provider. Close the provider process before switching threads. Discovery health-check processes do not acquire state. Use a separate directory per concurrently running T3 provider instance. Changing origin/operator/application/environment/model requires separate state. Authentication failure needs a restart with valid credentials.

## T3 installation and provider setup

Use the release pinned in `upstream-lock.json`: `0.0.46-nightly.20261008.2813`, commit `30cc788975500a8c00d32a50f348174d1ce578d1`. The verified macOS ARM64 archive is available from the [official release](https://github.com/pingdotgg/t3code/releases/tag/v0.0.46-nightly.20261008.2813). Verify its SHA-256 against the lock before extracting. No T3 fork is required; do not automatically update this POC installation.

Run T3 on loopback with an isolated private data directory:

```sh
/absolute/t3 --no-browser --host 127.0.0.1 --port 3777 --base-dir /absolute/private/t3-data
```

Open the pairing URL printed by T3 locally. In **Settings → Providers → Add provider → Local ACP command**:

- Executable: absolute path to `raptor-acp`.
- Arguments: two separate literal rows, `--config` and the absolute config path.
- Label: `Raptor Infra Ops`.
- Environment variables: none required.

Choose this provider in a new thread. Its initial discovery model may appear as **Default**; the bridge pins requests to `gpt-5.6-luna`. Application/environment selection comes from the config, not the prompt. The answer includes a Raptor request link. Runtime approvals and recovery stay in the Raptor request UI.

## Lifecycle and limits

The bridge persists the exact idempotency key and payload before submission. Reload reconnects to that request, replays text and progress, and checks request/operator/attempt bindings. An uncertain submission retries the same key and bytes. Success requires live execution, bound model/result evidence, verified checkpoint and confirmed cleanup. Cancellation records intent before calling Raptor; unresolved cleanup never appears as successful completion. Shutdown releases the state lock.

Text questions only: no file edits, local terminal, attachments, MCP execution, model overrides, delegation or rollback. Caller-supplied MCP commands are ignored. HTTPS origins only; `allowLoopbackHTTP: true` exists solely for synthetic loopback tests. Do not route production traffic over HTTP.

## Verification

```sh
go test -race ./...
go vet ./...
```

The official ACP SDK interoperability test uses only synthetic HTTP fixtures. Install the pinned dependencies under `tests/`, then:

```sh
node tests/acp_client.mjs /absolute/raptor-acp /absolute/tests/node_modules/@agentclientprotocol/sdk/dist/acp.js
```

See [acceptance evidence](docs/acceptance-2026-10-09.md). Deployed Raptor/Gateway acceptance requires an existing active operator credential file and functioning live environment; local fixtures do not prove cloud execution.
