# Request Conversation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add authenticated, durable text conversation to the existing Request progress view.

**Architecture:** Raptor admits messages and relays them through its transactional outbox. Gateway owns ordered delivery, receipts and request-specific continuation; Pi consumes plain-text input at safe boundaries. The existing browser-direct WebSocket remains read-only and carries message state alongside progress.

**Tech Stack:** Go/PostgreSQL, Node.js, pinned Pi 0.99.2, React/assistant-ui, existing Native and AX runtimes.

**Spec:** [Approved design](../specs/2026-10-09-request-conversation-design.md), approved by the user in this chat after commit fe959ef.

## Global Constraints

- Sender: Request creator or current admin; valid browser session and CSRF required on every POST.
- Message body exactly {messageId, text}; UUID message ID; valid UTF-8, nonblank, at most 2,000 Unicode code points and 8,192 UTF-8 bytes; maximum 20 outstanding messages per Request.
- Idempotency key is (requestId, messageId); conflicting actor or exact text returns 409; replay check precedes lifecycle rejection.
- Keep the original definition/hash immutable, the six application.question read tools, and existing operation/approval authorization.
- Preserve the global runtime slot, exclusive driver lease, cleanup fencing and no automatic replay of ambiguous inference.
- Each attempt retains 90 seconds, 10 turns and 1,024 output tokens per model call; new input does not reset attempt limits.
- Resume a validated Request-specific session/checkpoint; never use continueRecent or a client-supplied path.
- Old completed Requests without a recorded session association remain view-only.
- Additive migrations only; conversation disabled by default; no cloud provisioning or public Gateway write ingress.
- No new dependencies required. Keep frontend generated assets committed and verified.

## Review Focus

- A lost POST response followed by retry after completion must return one receipt, not create duplicate model input (Task 1).
- Outbox reordering or a permanently rejected sequence must not reorder or stall later valid messages (Task 2).
- Slash-like text and template syntax must arrive unchanged without invoking commands/skills (Task 3).
- A message arriving exactly as the harness settles must be consumed once or remain queued for continuation (Task 4).
- Navigation, logout or an already closed completion socket must not mix drafts/history or hide a newly accepted message (Task 5).

## Files and ownership

Keep new code focused rather than growing the existing worker, request service and JSX files substantially:
- Raptor requests/messages.go: admission/idempotency/history; messages_http.go: browser endpoints; existing outbox.go/gateway.go: transport; execution_view.go/progress.go: message projection.
- Gateway execution/conversation_models.go: wire/domain types; conversation_store.go: queue/receipts; conversation_worker.go: delivery/continuation transitions; httpapi/conversation.go: authenticated routes.
- Gateway runtime/conversation.go: bounded inbox/result/session wire helpers; Native/AX implementations call their existing transports.
- Harness conversation.mjs: validated inbox, literal input and turn receipts; existing live-question/live-runner/progress files retain their other responsibilities.
- assistant-ui conversation.mjs: message merge/send state; ConversationComposer.tsx: composer/user display; existing model/index/styles integrate it.

## Contract names used by all tasks

JSON message submission: {messageId, text}; relay: {messageId, requestId, actorId, inputSequence, text, acceptedAt}. Raptor assigns inputSequence monotonically under its Request lock. Gateway status is queued, delivered, answered, interrupted or rejected. Local UI adds sending, accepted and send_failed; these never assert Gateway processing.

Gateway execution package defines ConversationInput (relay fields), ConversationReceipt (messageId, inputSequence, status, attemptId, reason), ConversationCapability (version=1, enabled, canContinue, reason), ConversationSession (requestId, sessionId, sessionFile, checkpoint), ConversationTurn (turnId, messageIds, result LiveResult). Request ID and attempt ID must be checked on every decoded runtime payload. SessionFile is internal only, validated relative to the restored sessions directory and never exposed in public views.

Browser message receipt includes messageId, inputSequence, status, acceptedAt and a public reason. GET message history follows the existing signed-in Request read policy; writes enforce the narrower creator/admin policy. Pagination uses afterInputSequence, not the Gateway event cursor.

### Task 1: Raptor durable admission and outbox contract

**Files:** Create migrations/005_request_messages.sql under components/raptor; requests/messages.go, messages_http.go, messages_test.go, messages_http_test.go; modify requests/http.go, outbox.go, gateway.go, progress.go, execution_view.go, internal/backend/service.go, cmd/backend/main.go and internal/db/migrate.go as needed for configuration/migration validation.

**Interfaces:** Produce SubmitMessage(ctx context.Context, actor domain.User, requestID, messageID, text string) (MessageReceipt, error), ListMessages(ctx context.Context, requestID string, after int64) ([]PublicMessage, error), and MessageClient.PutMessage(ctx context.Context, requestID string, payload json.RawMessage) (MessageReceipt, error). Add ConversationEnabled on requests.Service from RAPTOR_CONVERSATION_ENABLED (default false); capability version=1 is also required for send admission.

- [ ] Write TestMessageAdmissionAuthorizationAndBounds, TestMessageLostAckRetryAfterCompletion, TestMessageCapacityAndSequence, TestMessageOutboxAtomicity. Assert 401 without login, 403 without CSRF or creator/admin permission, 400 for whitespace/unknown fields/2,001 code points/over 8,192 bytes; exact max valid text preserved; 202 accepted; same retry returns same sequence; conflicting actor/text 409; 21st outstanding message 429; rolled-back admission leaves neither message nor outbox row. Add migration idempotence and app-role privilege tests.
- [ ] Run `cd components/raptor && TEST_DATABASE_URL="$TASK_TEST_DATABASE_URL" go test ./internal/requests ./internal/backend ./internal/db -run 'Message|Migration'`; confirm new cases fail before implementation.
- [ ] Implement admission transaction and tables: request_messages stores immutable actor/text/hash/sequence/time and projected delivery status; Request row lock allocates sequence and checks capacity. Register POST/GET messages endpoints, exact-body decoding and public canSend/reason projection. Add explicit message outbox topic; transient failures remain pending, permanent delivery receipts update the row with a visible reason. Check capability through the service-authenticated Gateway endpoint; stale/absent capability never enables writes. Existing unsupported/direct Requests fail closed. Apply message lifecycle updates during progress sync so Raptor capacity is eventually released.
- [ ] Run the new tests and complete Raptor Go suite; expect zero failures. Commit `Add authorized durable Request message admission` with explicit files.

### Task 2: Gateway ordered queue and public receipts

**Files:** Create components/agent-gateway/migrations/004_conversation.sql, internal/execution/conversation_models.go, conversation_store.go, conversation_store_test.go, internal/httpapi/conversation.go, conversation_test.go; modify internal/httpapi/server.go, internal/db/migrate.go and cmd/gateway/main.go.

**Interfaces:** Produce Store.QueueMessage(ctx context.Context, in ConversationInput) (ConversationReceipt, error), PendingMessage(ctx context.Context, requestID, attemptID, owner string) (*ConversationInput, error), RecordMessageReceipt(ctx context.Context, attemptID, owner string, receipt ConversationReceipt) error, Conversation(ctx context.Context, requestID string) (ConversationCapability, error). GET /v1/requests/{id}/conversation returns capability; POST /v1/requests/{id}/messages accepts only authenticated service relay. GATEWAY_CONVERSATION_ENABLED defaults false and requires supported runtime wiring.

- [ ] Write TestConversationQueueOrderingAndGaps, TestConversationRejectedSequenceAdvances, TestConversationDedupAndCapacity, TestConversationServiceAuthAndBinding, TestConversationStatusReplay. Assert sequence 2 cannot execute before 1, rejected 1 is a durable skipped row, conflicting sequence/actor/hash is rejected, unknown Request is retryable without constructing an execution, capacity is 20, write tickets cannot invoke message routes, event/status transaction rolls back together and duplicate receipt appends no duplicate public event.
- [ ] Run `cd components/agent-gateway && TEST_DATABASE_URL="$TASK_TEST_DATABASE_URL" go test ./internal/execution ./internal/httpapi -run Conversation`; confirm failure before implementation.
- [ ] Implement tables conversation_sessions, conversation_messages and conversation_turns with bounded state transitions and Request/attempt associations. Queue late lifecycle rejections as durable rejected receipts with their input sequence, then return that receipt so the outbox can settle. Redact public message events and reject known credential-bearing input before model delivery; logs never print text. Use kind=message and stable messageId/status/role details. Capability exposes no private checkpoint paths. Unknown/malformed service payload returns a permanent error without creating a new execution.
- [ ] Run new tests and Gateway suite; expect zero failures. Commit `Persist ordered Gateway conversation messages and receipts`.

### Task 3: Literal Pi input and Native/AX delivery

**Files:** Create components/agent-harness/conversation.mjs, conversation.test.mjs; components/agent-gateway/internal/runtime/conversation.go, conversation_test.go; modify agent-harness/live-question.mjs, live-question.test.mjs, live-runner.mjs, runner.mjs, progress.mjs; Gateway internal/execution/live_worker.go, live_models.go, runtime/live.go, ax_live.go, ax_client.go and their existing fixture tests.

**Interfaces:** Add LiveRuntime.DeliverMessage(ctx context.Context, handle RuntimeHandle, input ConversationInput) error. Extend LiveStart with optional ConversationSession and InitialMessages; LiveObservation with SessionID, SessionFile and Turns []ConversationTurn. Add comparable scalar MessageID, InputSequence, TurnID, SessionID fields to RuntimeEvent as required; do not break existing struct-based deduplication with slice fields. Harness createConversationInbox({privateRoot, binding, session, onProgress, signal}) returns {poll, close}; shared wire uses one bounded JSON file per message and per turn, not a potentially oversized aggregate inbox.

- [ ] Write TestConversationRuntimeDeliveryNativeAX (same bindings and redaction for both), TestConversationInboxIdempotency, and real-SDK tests `literal slash/template input does not invoke extensions` and `input receipt precedes corresponding model reply`. Assert exact text equality, wrong Request/attempt/sequence rejects, duplicate delivery yields one session input, incomplete file is ignored until valid publication, deadline/abort closes inbox polling, private thinking never enters public output. Assert per-turn response files preserve full validated answers beyond the 2,048-byte narration bound without accepting unbounded data.
- [ ] Run `node --test components/agent-harness/conversation.test.mjs components/agent-harness/live-question.test.mjs` and `cd components/agent-gateway && go test ./internal/runtime -run Conversation`; confirm failures.
- [ ] Implement fixed private paths derived only from validated IDs, canonical payload hashes and bounded decoding. Runtime delivery writes a complete message file with retry-safe identity; harness consumes it once and emits received receipts only when input is appended. Use `session.prompt(text, {expandPromptTemplates:false, streamingBehavior:'steer'})` where verified SDK semantics support active runs; test that this path bypasses extension commands and templates. Validate session header/Request association on open. Keep current tools and per-attempt limits; publish validated per-turn final results separately and map receipts to stable message IDs. Add explicit session-created receipt for new conversations. Each answer must use current-turn scoped evidence; prior conversation observations cannot satisfy fresh-evidence requirements.
- [ ] Run all harness tests and Native/AX runtime regressions; expect zero failures. Commit `Deliver literal Request messages to pinned Pi sessions`.

### Task 4: Continuation, settle handshake and recovery fencing

**Files:** Create components/agent-gateway/internal/execution/conversation_worker.go, conversation_worker_test.go; modify live_worker.go, live_store.go, live_journal.go, ownership.go, retries.go and runtime/live.go/ax_live.go for Request-specific restore. Extend Task 2 tables only additively if extra receipt fields are required.

**Interfaces:** Produce Store.CloseConversationInput(ctx context.Context, attemptID, owner string, lastDeliveredSequence int64) error, QueueContinuation(ctx context.Context, requestID string) error and RequestConversationSession(ctx context.Context, requestID string) (ConversationSession, error). Worker uses Task 2 PendingMessage/RecordMessageReceipt and Task 3 DeliverMessage. Add a harness settling receipt with the last acknowledged input sequence; Gateway closes that attempt's delivery window transactionally. Unacknowledged writes stay queued and are excluded from a settled attempt's answered set.

- [ ] Write TestConversationSendAtSettleBoundary, TestConversationFreshAttemptAndExactSession, TestConversationLimitsDoNotReset, TestConversationInterruptedReceiptNoReplay, TestConversationCleanupBlocksContinuation, TestConversationOldHistoryViewOnly. Assert competing send/settle transactions produce one delivered input or one queued continuation; fresh attempt ID/access/deadline; exact Request session restored; missing/mismatched/path-traversing session rejects before inference; no overlap in global slot; lost lease fences delivery; model call with ambiguous receipt is not automatically replayed; final responses bind only acknowledged message IDs; initial question is not resubmitted during continuation.
- [ ] Run `cd components/agent-gateway && TEST_DATABASE_URL="$TASK_TEST_DATABASE_URL" go test ./internal/execution ./internal/runtime -run Conversation`; confirm new lifecycle tests fail before implementation.
- [ ] Integrate delivery into worker Poll and persist session/turn receipts atomically with public events. Only create continuation after verified Request checkpoint and all cleanup confirmations; keep global bootstrap selected_checkpoint independent. Re-fetch immutable binding/catalog and new live observations, issue new access, open the exact session and use pending user text as the continuation prompt. Close input before checkpointing via the settle handshake, not a time-based guess. Store every final answer, project latest valid result for compatibility, and checkpoint the latest Request session. Failed/cancelled/ambiguous attempts follow existing recovery rules; known unsent input remains visibly pending until resolved, never silently marked answered. Waiting approval states cannot be cleared by chat input.
- [ ] Run complete execution/runtime suites and the two-fresh-session harness integration test; expect zero failures. Commit `Resume Request conversations with fenced continuation attempts`.

### Task 5: assistant-ui composer and reconnecting message history

**Files:** Create components/raptor/worklog-ui/src/conversation.mjs, ConversationComposer.tsx and test/conversation.test.mjs; modify src/index.tsx, model.mjs, styles.css and test/view.test.mjs; modify components/raptor/web/poc/app.js, direct-progress.js and their tests; regenerate worklog.js/css through build.mjs.

**Interfaces:** conversation.mjs exports mergeConversationMessages({requestId, originalQuestion, acceptedMessages, events, localMessages}) and createMessageSender({requestId, submit, onChange}); sender.send(text) and sender.retry(messageId) retain identity after timeouts. Extend updateWorklog view with conversation={canSend,reason,messages,onSend}; keep existing mount/reset compatibility. POST uses the platform's current CSRF client. Gateway event sequence and Raptor input sequence remain separate.

- [ ] Write `composer sends once and timeout retry keeps messageId`, `accepted receipt merges with Gateway replay without duplicates`, `navigation/logout cannot replace another Request draft`, `completion socket renews after accepted input`, `readonly viewers cannot invoke onSend`, `original question and all final turns render in conversation order`. Test the actual generated React bundle with sending/accepted/queued/delivered/answered/interrupted/rejected labels, escaped text and existing group expansion preserved. Other viewers reconnect when saved-history refresh observes a new active attempt.
- [ ] Run `npm test` in worklog-ui and `node --test components/raptor/web/poc/*.test.mjs tests/harness/go-platform-web.test.mjs`; confirm new behavior fails first.
- [ ] Implement creator/admin-capability-gated ComposerPrimitive, UserMessage rendering, in-memory Request-scoped send state and clear drafts on logout. Keep text on retryable send failure; use the same UUID for lost-ack retry. Merge persisted messages by stable ID, label accepted rows awaiting delivery, show recorded final turns, and keep execution/cleanup separate from conversation state. On successful admission renew direct progress with its valid Gateway cursor; preserve renewal, HTTP saved-history fallback and navigation cancellation. Generate assets with `npm run build`.
- [ ] Run frontend tests plus `npm run check:assets`; expect zero failures. Commit `Enable authenticated Request conversation in assistant-ui`.

### Task 6: Integrated acceptance, rollout documentation and PR

**Files:** Create components/agent-gateway/internal/acceptance/conversation_test.go and components/raptor/internal/acceptance/conversation_test.go; update .github/workflows/platform-images.yml, components/agent-gateway/PROGRESS_STREAM.md, components/agent-gateway/docs/live-question-runbook.md and components/raptor/worklog-ui/README.md. Store new browser evidence under root working/implementation, never archive/materials.

**Interfaces:** Use the prior tasks' real HTTP endpoints and deterministic Pi provider fixtures. No parallel independent mock contract substitutes for production message/receipt behavior.

- [ ] Write TestConversationEndToEndOutboxAndReplay and TestConversationContinuationRestoresRequestOnly. Exercise authenticated POST -> real outbox -> Gateway -> pinned SDK -> ordered WebSocket replay -> final answer, plus lost acknowledgement and a fresh sandbox/attempt continuation. Ensure the prior Request transcript, original binding and cleanup status survive. Cover feature flag disabled, incompatible runtime and legacy completed Request combinations.
- [ ] Run those acceptance tests against a disposable local PostgreSQL URL; confirm they fail for any missing integration before finishing wiring.
- [ ] Complete integration only as needed; add both feature flags/capability/rollout order to runbooks and CI checks. Keep public ingress read-only; apply schema migrations through existing schema-owner flow. Local fixtures and screenshots are labeled as such; enabling in a deployed environment requires the separate reviewed runtime acceptance specified in the design.
- [ ] Run both Go suites and go vet, all harness/page tests, worklog-ui tests/build consistency, and CI container packaging checks. In the local browser verify send, failure retry, reload history, live tool updates and continuation; record screenshots. Expect all checks green without new dependencies or unrelated Terraform changes.
- [ ] Request whole-branch review, address findings, commit, push a PR against current main and attach it to this task. User authorization to self-merge persists; merge only after review and CI pass, then verify the merged commit. Preserve deployment limitations in the final report.

## Self-review and execution handoff

Coverage: admission/idempotency/capacity -> Task 1; durable ordering/status/redaction -> Task 2; literal input/Native-AX receipts -> Task 3; exact-session checkpoint/settle/recovery/fresh access -> Task 4; composer/history/cursor/isolation -> Task 5; feature flags and integrated runtime evidence -> Task 6. All five Review Focus cases have owning tests. Contracts and status names above are shared across tasks.

Recommended execution method: Native, sequential implementation in this session, with one independent whole-branch review after the integrated tests. These six tasks share queue, receipt and lifecycle interfaces; one implementer can keep those consistent without repeated context transfers. This recommendation awaits the user's choice and plan review before code implementation.
