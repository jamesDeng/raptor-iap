# Browser-direct progress

Raptor's authenticated, CSRF-protected `POST /api/v1/requests/{id}/progress-ticket` validates the request using the current POC's signed-in request-read policy. It calls Gateway's service-authenticated `POST /v1/requests/{id}/progress-ticket` with the user ID and browser origin. Local direct requests use their existing polling path.

Gateway issues 256-bit random, opaque tickets scoped to one request, user and exact origin. Only a SHA256 hash is persisted. Tickets expire after 60 seconds and are atomically consumed once. They authorize progress reads only. The browser opens `/v1/progress` and sends `{type:"subscribe",token,requestId,afterSequence}` as its first frame within five seconds; tickets never appear in a URL or storage. Gateway emits `event`, public `snapshot`, and `complete` frames. Input definitions and private checkpoint references are excluded from snapshots.

Replay uses Gateway's per-request sequence, starting at zero on page open. During initial stream outages, saved Raptor history is read separately; a successful stream replaces it with Gateway replay. Raptor's saved-event sequence is a different namespace and must never be used for Gateway replay. Reconnect on the same page uses the last Gateway sequence. Browser deduplicates by sequence, ignores obsolete connections, and cancels on navigation/logout. Connection leases last at most 60 seconds; renewal asks Raptor for a new ticket and revalidates the session. Logout in another tab may leave an existing read-only connection active until that lease expires. There is no immediate cross-service revocation endpoint yet.

Gateway reads committed event rows every 500ms and drains replay pages before sending the execution snapshot. Browser delivery uses WebSocket; this implementation does not yet use database LISTEN/NOTIFY. Waiting states remain subscribed; terminal executions only send `complete` after all three cleanup confirmations and no recovery requirement. Connections have a 128-slot cap, a 2048-byte incoming frame limit and five-second writes. Subsequent client application messages close the connection; mutations remain existing Raptor HTTP operations.

## Configuration (disabled by default)

- Gateway `GATEWAY_PROGRESS_PUBLIC_URL`: `wss://gateway.example/v1/progress`.
- Gateway `GATEWAY_PROGRESS_ORIGINS`: comma-separated exact frontend origins, e.g. `https://raptor.example`. No wildcard.
- Raptor frontend `RAPTOR_PROGRESS_CONNECT_ORIGIN`: `wss://gateway.example` (origin only), explicitly added to CSP.
- Run Gateway schema-owner migrations, including `003_progress_tickets.sql`. Ticket table belongs to Gateway; application role needs SELECT/INSERT/DELETE only.

Non-TLS WebSocket is allowed only for localhost development. Public Gateway ingress must route only the browser progress endpoint to the intended service with TLS and WebSocket upgrades; do not expose all service-authenticated administrative routes as a shortcut. No ingress or cloud resource is provisioned by this change.

When realtime is explicitly disabled, the browser shows a polling label and retains the existing HTTP history path. Authentication/connection failures in an enabled stream show reconnecting/unavailable; they do not silently downgrade. Frontend rendering uses the assistant-ui transcript with public narration and expandable tool groups. This transport does not implement conversational agent messages, live approval/resume, permission grants or PR execution.
