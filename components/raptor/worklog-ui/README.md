# Raptor read-only request progress with assistant-ui

The request Progress tab uses the pinned `@assistant-ui/react@0.15.25` runtime, thread and grouped message primitives. Raptor authenticates the viewer, obtains a short-lived Gateway ticket, and the existing direct-progress controller subscribes to sandbox-gateway. No assistant-ui server or T3 backend is required for this view.

Narration is displayed inline, with consecutive tool calls grouped into expandable activity rows. Each tool shows its recorded start/result, outcome, timestamps and evidence mode. Content is rendered as escaped text. The viewer has no composer, message editing or execution commands; approvals stay in the platform.

Public progress events are mapped to stable message and tool IDs. Because the current journal has tool names but no provider call IDs, a result is paired only with exactly one unmatched start of that name in the same contiguous attempt segment. Ambiguous and orphan results remain recorded narration. Failed, cancelled, pending and unknown outcomes never receive a success checkmark. Previous attempts with missing results are not marked running after a new attempt starts.

Only actual event summaries are shown. The viewer does not invent agent reasoning, skill-loading narration or approval transitions. Richer narration requires the producer to emit public progress summaries. Existing snapshot status, checkpoint, cleanup and recovery information remains independent of an answer. Timestamps provide a recorded activity span, not estimated total run time.

The existing controller owns reconnection, ticket renewal, ordered replay, request navigation and saved-history fallback. The mounting exports `updateWorklog` / `resetWorklog` and generated asset paths remain stable for that integration; the active viewer no longer imports T3 WorkLog. The old vendored source is retained as provenance and is not bundled.

## Build and verification

```sh
npm ci --ignore-scripts
npm run build
npm test
npm run check:assets
```

Commit the generated `../web/poc/worklog.js` and `worklog.css` so the existing Go embed and container build needs no Node runtime. CSS is scoped to `.assistant-progress` and matches the approved dark probe. It explicitly resets platform summary weights, open-summary margins and pre backgrounds inside the host. The surrounding Request shell keeps its platform theme.

From repository root:

```sh
node --test components/raptor/web/poc/*.test.mjs tests/harness/go-platform-web.test.mjs
```

The jsdom component test exercises the real production bundle, expansion preservation, escaped content and readonly display. Layout observer/scroll shims are confined to tests; actual browser scrolling is checked separately.

Local acceptance used the existing completed live request via the read-only preview on port 3790. That preview explicitly disables realtime and uses saved-history polling; it does not validate production WebSocket ingress or run new operations. The direct stream controller regression tests cover subscription, replay deduplication, renewal and navigation isolation. This change does not deploy services or implement new approval/PR workflows.

Sources: [ExternalStoreRuntime](https://www.assistant-ui.com/docs/runtimes/custom/external-store), [grouped activity](https://www.assistant-ui.com/docs/guides/chain-of-thought), and the installed package types/source.

Request conversation adds assistant-ui ComposerPrimitive and UserMessage
rendering while keeping the production progress/tool view. The platform passes
`conversation={canSend,reason,onSend}` to updateWorklog; saved message history
comes from `snapshot.conversation.messages`. onSend uses the existing Raptor
CSRF API, never the browser progress ticket. Local send state is in memory and
is retained per Request across navigation and destroyed on logout. Timeout retries retain a stable UUID.
Accepted, queued, delivered, answered, rejected and interrupted receipts merge
by message ID; per-turn answers remain in chronological transcript history.
Neither text input nor the UI grants operation permissions.

Both deployment flags default off. See the Gateway live-question runbook for
migration order, runtime capability and required deployed acceptance. The
local conversation preview uses simulated messages, not real infrastructure.
