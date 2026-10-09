# T3 Code ACP Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. No delegation is authorized yet; native execution is recommended for this plan.

**Goal:** Submit a real read-only Raptor application question from T3 Code, render its bound progress/answer and confirm checkpoint and runtime cleanup.

**Architecture:** A standalone Go ACP provider command connects T3 to Raptor's existing browser-authenticated HTTP API. Gateway remains responsible for Pi execution, scoped credentials, runtime choice, checkpoints and cleanup. The bridge stores private session/request mappings and never invokes Pi locally.

**Tech Stack:** Go standard library, ACP v1 over newline-delimited JSON-RPC, existing Raptor Go HTTP APIs, pinned T3 Code distribution.

**Spec:** [Approved design](../designs/2026-10-08-t3code-integration.md).

## Global Constraints

- Add a standalone Go component `components/t3code-bridge/`, with its own module, executable, ACP transport, Raptor HTTP client, session store and tests.
- Model is `gpt-5.6-luna`; each prompt uses operation `application.question` and trusted configured application/environment selectors.
- Pi remains in the Gateway-selected runtime; no runtime cutover, model credentials on the T3 host, infrastructure writes or T3 source fork.
- Credentials stay outside Git, ACP prompts and launch arguments. Private state directories use 0700; files use 0600.
- Reject unsupported attachments, local filesystem/terminal operations, model overrides, delegation and rollback. T3-supplied MCP definitions are not executed or forwarded; the bridge advertises no MCP capability.
- Treat final answer, checkpoint verification and cleanup as separate obligations. Success requires all three, matching binding/model, live evidence and no recovery flag.
- Pin a release verified to support Local ACP commands. Initial candidate: `v0.0.46-nightly.20261008.2813` (release reports commit prefix `30cc78897550`); its tag-scoped provider documentation was fetched successfully. Resolve full commit, download checksum and actual runtime compatibility before claiming a supported pin. Do not substitute `latest`.
- Preserve archive/material snapshots and unrelated staged edits. Create an isolated worktree from the compatible `ack-ax-migration` revision `68ccc84`, after checking any newer source needed for deployment compatibility.
- Per-request API timeouts: 15 seconds; response/message size limit: 1 MiB; polling interval: 2 seconds; observation deadline: 10 minutes; cancellation observation deadline: 60 seconds. Deadline expiry retains unresolved mappings and does not claim cleanup.

## Review Focus

1. Client disconnect after accepted POST: reuse the exact persisted payload/idempotency key; never silently submit a new operation.
2. Two bridge processes or concurrent prompts: private state lock and per-session busy guard prevent overlapping ownership.
3. Session reload: replay complete persisted conversation, observe unresolved requests without resubmission, and refuse changed origin/selectors/model/user identity.
4. Hostile or stale responses: reject wrong binding, cursor regressions, redirects, oversized bodies and terminal results lacking verified cleanup.
5. Cancellation racing submission/completion: persist cancellation intent before forwarding, resolve uncertain submission first, and retain truthful terminal outcomes.

## Task 1: Authenticated Raptor client and private configuration

**Files:** Create `components/t3code-bridge/go.mod`, `cmd/raptor-acp/main.go`, `internal/raptor/{client,contracts,client_test}.go`, `internal/config/{config,config_test}.go`.

**Interfaces:**
- `config.Load(path string) (Config, error)`; Config fields `Origin`, `ApplicationCode`, `EnvironmentCode`, `Model`, `CredentialsFile`, `StateDir`, `AllowLoopbackHTTP`.
- `raptor.NewClient(config Config) (*Client, error)`; `Login(ctx context.Context) (userID string, err error)` loads a private username/password file and retains cookies/CSRF in memory only.
- `Create(ctx context.Context, key string, payload json.RawMessage) (Request, error)`, `View(ctx context.Context, requestID string) (RequestView, error)`, `Events(ctx context.Context, requestID string, after int64) (Timeline, error)`, `Cancel(ctx context.Context, requestID string) error`.
- Wire DTOs follow the inspected Raptor sources, including RequestView availability/staleness, Timeline paging, execution `checkpointStatus:"verified"` and cleanup booleans. No Gateway internal credentials.

- [ ] Write failing httptest cases `TestLoginCookieAndCSRF`, `TestPinnedQuestionPayload`, `TestRedirectNeverReceivesCredential`, `TestOnlyExplicitLoopbackHTTP`, `TestRejectOversizedAndMalformedResponse`, `TestExpiredSessionIsNeedsLogin`, `TestPrivateConfigRejectsSymlinkAndBroadModes`. Assert GET has the login cookie, POST has cookie plus X-CSRF-Token; model and selectors cannot come from prompt text.
- [ ] Run `go test ./internal/config ./internal/raptor` in the new component and confirm the expected missing implementation failures.
- [ ] Implement the interfaces with restricted origins, fixed endpoint paths, URL-escaped IDs, no redirect following, bounded bodies and generic redacted errors. Verify Login user identity and absence of credential values from diagnostics.
- [ ] Run the same tests; require PASS. Commit component/client files with `feat: add authenticated Raptor bridge client`.

## Task 2: Durable session and request lifecycle

**Files:** Create `internal/state/{store,store_test}.go`, `internal/bridge/{runner,validation,runner_test}.go`.

**Interfaces:**
- `state.Open(dir string) (*Store, error)` obtains an exclusive process lock; `Create(binding Binding) (Session, error)`, `Load(id string, binding Binding) (Session, error)`, `Save(session Session) error`, `Close() error`.
- Binding includes Raptor origin, authenticated user ID, application/environment/model. Session persists transcript, exact submission bytes/key, returned request ID, observed attempt ID, cursor, cancellation intent and terminal outcome. Atomic rename plus file/directory sync; refuse symlink/path traversal and corrupt state.
- `bridge.New(client *raptor.Client, store *state.Store, config config.Config) *Runner`; `NewSession(ctx context.Context) (string, error)`, `LoadSession(ctx context.Context, id string, emit func(Update) error) error`, `Prompt(ctx context.Context, id string, text string, emit func(Update) error) (Outcome, error)`, `Cancel(ctx context.Context, id string) error`.
- Outcome distinguishes verified completion, confirmed cancellation, failed, blocked and unresolved. Update carries progress tool-call state or assistant text; no synthetic reasoning/token stream.

- [ ] Write failing tests `TestAcceptedPostLostResponseReusesExactKeyAndBytes`, `TestReloadObservesWithoutResubmitting`, `TestIdentityChangeRefused`, `TestStateExclusiveLock`, `TestConcurrentPromptRejected`, `TestCursorOrderingAndPaging`, `TestWrongAttemptModelAndSelectorsRejected`, `TestAnswerRequiresVerifiedCheckpointAndCleanup`, `TestUnavailableOrStaleViewCannotComplete`, `TestCancelBeforeSubmissionResponse`, `TestCancelCompletionRace`, `TestDeadlineRetainsUnresolvedRequest`.
- [ ] Run `go test ./internal/state ./internal/bridge`; confirm expected failures before implementation.
- [ ] Implement persistent intent before HTTP writes and transcript/update persistence before emission. A second prompt cannot proceed while a prior request is unresolved. Reload may retry an uncertain POST only using its saved exact payload/key, then observe the resulting ID. Never restart an existing remote attempt.
- [ ] Map authoritative Raptor execution to outcome: live completed result must match request, pinned attempt/model/selectors, nonempty valid evidence/usage, checkpoint verified, all cleanup booleans true, no recovery flag and fresh execution projection. Failure/blocked states stay visible without successful completion. Cancellation waits for authoritative cleanup; a local abort alone is unresolved.
- [ ] Run tests; require PASS. Commit with `feat: persist bridge request lifecycle and cleanup outcomes`.

## Task 3: ACP provider executable

**Files:** Create `internal/acp/{server,protocol,server_test}.go`; finish `cmd/raptor-acp/main.go`; create `tests/acp_client.mjs`, `upstream-lock.json`.

**Interfaces:** `acp.Serve(ctx context.Context, input io.Reader, output io.Writer, runner *bridge.Runner) error` supports `initialize`, `session/new`, `session/load`, `session/prompt`, `session/cancel`; newline-delimited JSON-RPC with integer/string request IDs and notification handling.

- [ ] Resolve and pin upstream ACP v1 schema revision and a compatible ACP client SDK version in `upstream-lock.json`; use the SDK only for protocol verification, not a runtime dependency. Record T3 candidate full revision/checksum after inspection.
- [ ] Write failing protocol tests `TestNegotiatesV1FromV2Client`, `TestInitializeBeforeSession`, `TestNewAndLoadReplayTranscript`, `TestPromptProgressBeforeResponse`, `TestCancelNotificationWhilePromptPending`, `TestUnsupportedContentAndMethods`, `TestMcpDefinitionsNeverExecuted`, `TestStdoutContainsOnlyJSONRPC`, `TestBadFrameAndOversizedInput`, `TestBrokenPipeRetainsRemoteRequest`.
- [ ] Run `go test ./internal/acp`; confirm expected failures before implementation.
- [ ] Implement ACP v1 with loadSession and text-only prompt capabilities, fixed model selection, no file/terminal/MCP calls, serialized output and concurrent input handling so cancel can arrive during a prompt. Emit protocol-valid progress using tool_call/tool_call_update; emit verified final answer with request link. Failed/blocked/unresolved runs return explicit ACP errors or failed tool states, never a misleading normal-success answer.
- [ ] Wire `raptor-acp --config /absolute/private/config.json`; no startup banner on stdout. Validate text length/UTF-8 with Raptor's limits before submitting. Follow-ups are independent questions and show that fact when opening a session.
- [ ] Run `go test -race ./...`, build with `go build ./cmd/raptor-acp`, then verify executable against the pinned ACP client SDK for new/prompt/cancel/load. Commit with `feat: expose Raptor execution through ACP`.

## Task 4: T3 registration and real acceptance

**Files:** Create `components/t3code-bridge/README.md`, `docs/setup/t3code-bridge.md`, `components/t3code-bridge/config.example.json`, `docs/setup/t3code-bridge-acceptance.md`; link setup from repository README.

**Interfaces:** The installed T3 Local ACP command points to the bridge binary with only `--config` and a private file path as literal arguments. Raptor login credentials and model tokens are never entered in T3 provider environment settings.

- [ ] Run full bridge verification and inspect all changes for accidentally tracked secrets/state. If shared code was changed, also run its existing regression tests; no shared changes are planned.
- [ ] Install/run the pinned T3 candidate in an isolated local data directory. Confirm actual Local ACP command registration and v1 fallback using the bridge. Disable automatic upgrades for this acceptance. If release behavior differs from tag documentation, record blocker and fix the pin before continuing.
- [ ] Configure actual existing catalog selectors and private operator credentials without printing them. Submit one read-only question in T3; verify matching request/attempt in Raptor, real tool evidence, verified checkpoint and independently confirmed cleanup. Save sanitized receipt and exact software revisions.
- [ ] Exercise cancellation through the actual T3 client; independently verify the Gateway outcome and cleanup. Reload the T3 thread and confirm no duplicate Raptor request or sandbox execution. Keep unresolved cancellation visible if it cannot be demonstrated.
- [ ] Document exact build/registration steps, private config schema, fresh-question semantics, Raptor request links and truthful failure display. Acceptance table distinguishes protocol fixtures, actual T3 UI checks and real model/runtime checks.
- [ ] Run final `go test -race ./...`, `go vet ./...`, build; inspect git diff/status. Commit with `docs: record T3 bridge setup and verified acceptance`. Integration is complete only after actual T3 and real read-only execution acceptance; otherwise report the precise remaining boundary.

## Self-review

Four tasks cover the approved spec: private configuration/auth; durable binding and lifecycle; ACP transport; actual T3/model acceptance. Review Focus conditions each have named tests in Tasks 1–3. No task modifies Terraform, runtime selection, Infra API mutation gates or Pi version. Task 4 is explicitly live acceptance rather than a source-only claim.

## Execution handoff

Recommended method: native execution in this chat. The tasks depend tightly on shared session/lifecycle contracts; implementing sequentially avoids delegation overhead. Use one final independent review under the execution skill. Await plan review and execution-method choice before creating the implementation worktree or product code.
