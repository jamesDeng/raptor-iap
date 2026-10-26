# Replacement initial read gate

## Intent

Make a fresh PgCat replacement request acquire a valid, persisted two-node fleet baseline before the model can call a state-changing tool. The live `04dfc8e3-1845-4de6-a685-75a8ffd25492` attempt failed before fleet mutation: its first tool round included five reads and `db_proxy_node_protection_set`; the latter had the sanitized `sdk-arguments` category. The exact malformed field is not known. The first-round mutation itself is independently unsafe because the baseline did not exist.

## Approaches

1. Prompt-only ordering is small but cannot prevent another parallel first-round mutation.
2. Stage the existing Pi tool catalog: advertise only reads until the replacement trace persists a valid exact two-node fleet baseline, then expose the existing approval and mutation tools on the next turn. This is the chosen approach because it removes the premature action path while preserving the existing request-bound API checks.
3. Move the whole replacement workflow into deterministic controller code. That is larger than this diagnosis supports and would change the POC architecture.

## Design

- On a new replacement session without a saved baseline, activate only the existing catalog, observation and wait tools. Keep every mutation and approval-write tool unavailable in that stage. A resumed session with a validated saved baseline may start with the full existing catalog.
- After a completed tool round, unlock the full catalog only if `ReplacementTrace` has persisted a valid baseline from a fresh fleet observation. Do not unlock after malformed, stale, one-node, unhealthy or wrong-group data. Do not bypass the existing read queue, request-bound selectors, human approval, scale intent journal, or server authorization.
- Add a clear first-read instruction to the replacement prompt. This helps the model use the available tools, while the staged catalog supplies the enforcement.
- If a model attempts an unavailable action anyway, fail closed. No automatic retry of the same failed request, no fleet recovery by inference.

## Verification and limits

Use a synthetic Pi session to assert the initial catalog contains no mutation or approval-write tools and that a valid fleet baseline unlocks them on the following turn. Assert an invalid fleet read leaves them unavailable. Run the relevant harness tests and review the exact diff. Deploy only via the existing reviewed image and GitOps pin path. Before a fresh live attempt, verify Gateway false/idle, two ready clients, exact provider fleet and retained capture setup. A successful traffic capture alone is not replacement PASS.
