# T3 Code integration

See the [bridge setup and lifecycle guide](../../components/t3code-bridge/README.md) and [example configuration](../../components/t3code-bridge/config.example.json).

The implementation is a standalone ACP provider. T3 calls the provider; authenticated Raptor creates a read-only application question using the existing Gateway and sandbox path. The bridge does not provision cloud resources or change runtime routing.

This POC supports one open T3 thread per configured bridge state directory. Discovery processes may coexist; session processes require exclusive state ownership. The model and application/environment selectors are pinned in private configuration. Each prompt creates an independent remote question.
