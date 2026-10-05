# Raptor Infra API reader

Set the selected environment's `config.infraApiUrl` to the HTTPS gateway base URL and `config.clusterId` to its registered ACK cluster. Local development permits a loopback HTTP URL. Put only a secret reference in catalog configuration; actual `INFRA_USERNAME`, `INFRA_PASSWORD` and `INFRA_AUTH_HEADER` come from the backend's private runtime configuration. Gateway mode uses `X-Infra-Authorization`.

Missing credentials leave the reader unavailable. Explicit `RAPTOR_SIMULATION=true` plus `RAPTOR_FIXTURE_FILE` retains simulated discovery. No live restart command is wired by this slice. `HTTPInfra.GetDeploymentStatus` resolves the environment through the configured catalog resolver and validates cluster and workload UID.
