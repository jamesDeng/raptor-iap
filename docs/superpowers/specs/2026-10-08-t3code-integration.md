# T3 Code integration design

Date: 2026-10-08 (Asia/Shanghai).
Status: written design approved by the user; implementation plan awaiting review.

## Outcome

Use T3 Code as an additional operator conversation UI for this POC. A local ACP
provider command submits application questions to Raptor, shows durable progress
and the bound answer, and forwards cancellation. Raptor and Gateway remain the
owners of request validation, identity, execution and runtime cleanup. Pi runs
inside the runtime selected by Gateway, including Aliyun or AX.

The user authorized integration after the source-based research. The ACP bridge
is a refinement discovered during implementation exploration, not an already
implemented or tested integration.

## Approach and alternatives

Choose an ACP bridge using T3's documented Local ACP command support. It needs
no T3 source fork, model credential on the UI host or changes to the sandbox Pi
process ownership. The executable speaks ACP over stdin/stdout; HTTP access to
Raptor stays inside that executable.

The alternative of running T3's native Pi provider inside each actor introduces
T3 server/state persistence and competing Pi lifecycle ownership. A custom T3
ProviderAdapter would offer tighter presentation but require maintaining a fork.
Both alternatives are deferred.

Flow: T3 client → T3 server → Raptor ACP bridge → authenticated Raptor request API
→ Gateway → selected sandbox runtime → Pi → request-bound Raptor/Infra API tools.
Answers and progress return through Raptor's public execution projection.

## Component boundaries

- Add a standalone Go component `components/t3code-bridge/`, with its own module,
  executable, ACP transport, Raptor HTTP client, session store and tests.
- Add component setup documentation and an opt-in launch configuration. Pin a
  release verified to support Local ACP commands; do not rely on moving latest.
- Keep Raptor's catalog, approval UI and existing request pages available. Each
  T3 submission displays a Raptor request link and its selected app/environment.
- No provisioned resources or runtime cutover are required for the initial
  integration. Cloud resource additions, if later needed, use Terraform.

## Initial supported behavior

1. Operator config pins Raptor origin, application code, environment code and
   the existing allowed model `gpt-5.6-luna`. Prompt text cannot override them.
2. Authenticate as an existing Raptor operator using its login/session and CSRF
   contracts. Credentials come from a private file outside Git, not ACP prompts
   or launch arguments. Bridge data and session mappings have private modes.
3. Each plain-text prompt creates one `application.question` request using the
   existing request schema. Subsequent prompts create fresh bound requests;
   this does not promise persistent model conversation across questions.
4. Persist an idempotency key and exact payload before submission. On uncertain
   submission, retry only that payload/key; never create an alternate request.
   Resume mappings survive process restart and detect configuration mismatch.
5. Poll existing request/events APIs using their sequence cursor. Validate IDs,
   bound attempt, selected model and selectors before presenting results.
   Show progress as progress; do not fabricate live token streaming when the
   backend only supplies status events and a final answer.
6. Finish successfully only when the authoritative execution reports success,
   valid bound result, saved checkpoint and confirmed cleanup/access revocation.
   An answer with failed or unknown cleanup remains failed/blocked.
7. Forward ACP cancellation to the existing Raptor cancel action. A cancellation
   request is not proof of termination. Retain its request mapping for later
   observation if the bridge disconnects or the cancellation deadline expires.
8. Advertise only implemented ACP capabilities. Reject attachments, arbitrary
   filesystem/terminal requests, model overrides, delegation and rollback.
   This first integration uses the existing read-only question execution path.
   Infrastructure mutation approvals remain on Raptor's request page.

## Error handling and confidentiality

ACP stdout contains only protocol messages; diagnostics go to stderr with
credentials, cookies, CSRF values and private request contents redacted.
Authenticate TLS endpoints normally. Permit HTTP only for explicit loopback
development. Refuse redirects so credentials cannot move to another origin.
Use bounded bodies, calls and polling. Do not bypass Raptor browser auth using
Gateway's broad internal Basic Auth. An expired Raptor session requires renewed
login; it must not trigger duplicate submission or automatic sandbox replay.

## Verification and acceptance

- Protocol tests use an ACP client and cover initialization, session creation,
  prompt, loading a persisted mapping, progress, cancellation and unsupported
  capabilities. Verify against the pinned upstream ACP schema.
- HTTP integration tests exercise real handlers or faithful contract fixtures:
  session/CSRF rejection, duplicate submission, uncertain POST recovery, event
  cursor ordering, wrong request/attempt/model, stale cancellation and malformed
  or oversized responses.
- Execution projection tests cover successful answer plus cleanup, answer with
  failed checkpoint, unconfirmed cleanup, expired session and restart without
  resubmission. Success must never be inferred from answer text.
- Run the bridge through T3's actual Local ACP command provider. Submit a real
  read-only application question; confirm the same request/attempt in Raptor,
  tool evidence, checkpoint and cleanup. Test cancellation with independent
  gateway outcome evidence. Keep synthetic checks and live receipts separate.

## Source basis and current-state limits

Upstream reviewed on main on October 8, not yet pinned or run locally:
[Local ACP command](https://github.com/pingdotgg/t3code/blob/main/docs/user/providers-acp.md#add-a-local-command),
[Pi adapter](https://github.com/pingdotgg/t3code/blob/main/apps/server/src/orchestration-v2/Adapters/PiAdapterV2.ts),
[MIT license](https://github.com/pingdotgg/t3code/blob/main/LICENSE).

Local source: `raptor-iap/.raptor-local/worktrees/ack-ax-migration/` at `68ccc84`;
`components/raptor/internal/requests/{http,question_test,execution_view,actions}.go`,
`components/raptor/internal/auth/http.go`,
`components/agent-gateway/internal/execution/live_worker.go`, and
`docs/setup/ecs-ax-execution-ledger.md`. The ledger records a later ECS/K3s AX
direction and multiple runtime providers, superseding the earlier managed-ACK
design. Its live acceptance claims are recorded evidence, not independently
re-executed by this research. Active foundation worktree has staged unrelated
UI edits; integration must use a separate worktree based on compatible source.
