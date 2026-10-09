# Integration acceptance — 2026-10-08

## Independent verification

- The exact T3 release archive SHA-256 matched `upstream-lock.json`; its CLI started an isolated loopback server on macOS ARM64.
- Official ACP SDK 1.7.0 fixture test exercised new prompt, process restart/reload without duplicate creation, confirmed cancellation and non-execution of supplied MCP commands.
- Go race suite verifies HTTP auth, idempotency, state ownership, result validation, cancellation and ACP dispatch. Review identified cancellation admission, signal shutdown and lifecycle replay bugs; each was fixed with regression coverage.

## Acceptance boundary

The actual T3 UI was paired and a Local ACP command registered against a clearly named synthetic Raptor fixture. At 23:41 Asia/Shanghai, T3 displayed the synthetic answer, request link `fixture-request-1`, application/environment selectors and Completed progress. Earlier failures exposed two compatibility requirements: discovery must not acquire the session lock, and the pinned T3 `<user_request>` envelope must be extracted before applying Raptor text limits. Both are implemented. This proves actual T3 transport/rendering against a fixture, not cloud execution.

A deployed T3 → Raptor → Gateway → sandbox → Pi acceptance run is pending an active existing operator credential file. The historical temporary acceptance user is disabled; it was not reactivated. Historical cloud receipts are not evidence for this new bridge.

No cloud resources, Terraform definitions or Gateway provider routing were changed.

[2026-10-09 live acceptance](acceptance-2026-10-09.md) supersedes the pending-credentials boundary above.
