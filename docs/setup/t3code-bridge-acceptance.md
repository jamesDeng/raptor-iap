# T3 acceptance

[Live evidence and limits](../../components/t3code-bridge/docs/acceptance-2026-10-09.md) · [receipt](../../components/t3code-bridge/docs/acceptance-2026-10-09.json).

| Check | Result |
|---|---|
| Race suite, vet, build, official ACP SDK | Passed |
| Actual T3 + deployed Raptor/Gateway/model | Completed on raptor-backend/rdev.ali |
| Actual T3 cancellation | Cancelled with verified checkpoint and Raptor-confirmed cleanup |
| Actual T3 reload | History preserved; zero new requests |
| Independent node-level absence audit | SSH timed out |

Private local start script: primary repository .raptor-local/t3code-live/start.sh. Loopback port 3779. One open thread per provider state directory.
