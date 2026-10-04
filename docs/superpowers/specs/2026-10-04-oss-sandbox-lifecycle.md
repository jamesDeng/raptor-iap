# OSS-backed Pi sandbox lifecycle — proposed first integration

Status: approved by the user on October 4, 2026 ("go ahead" in reply to the written-design review). This is the approved pre-implementation design snapshot. Current implementation evidence and limits are in [the operator guide](../../setup/oss-sandbox-lifecycle.md).

## Intended outcome and scope

Turn the verified throwaway sequence into a reusable POC command: submit a small request, start a Singapore sandbox, restore the saved Pi credentials, run Pi, save a verified checkpoint, and confirm sandbox termination. A second request uses a replacement sandbox without browser authorization while the renewable session remains valid.

Carry forward direct user choices: one public raptor-iap repository with component folders; Pi calls OpenAI directly; GPT-5.6 Luna and ChatGPT Pro sign-in; local Pi files plus OSS-managed AES256 checkpoints; one active run per account; Aliyun total below RMB1,000. The accepted loss of work and token changes after the last completed checkpoint remains a limitation.

The first acceptance request uses a generated file and Pi's read tool. It does not deploy RDS, PgCat, ACK, Argo CD or applications. The controller manages lifecycle only, without an end-to-end infrastructure workflow. Infrastructure command selection remains future agent/Infra API integration.

## Starting evidence

- The October 4 real-login probe passed A → B → C. B refreshed a real token and C used the replacement credentials without sign-in. All three read-tool results and exact answers passed. Ten object headers reported AES256. Compute and temporary control keys were removed; the storage and scoped role were retained.
- Direct AuthStorage operation on the OSS mount previously failed locking and permission checks. Live auth and sessions must stay on the sandbox's local filesystem.
- The published Pi image is verified separately. The successful persistence probe used the official Gen2 template with pinned Node/Pi installation, not the custom image template. Do not quietly replace that tested runtime in this slice.
- Repository code currently covers the Pi image and AgenticFS setup, not a runtime orchestrator. AgenticFS tools stay intact and are not runtime dependencies of this OSS path.

Historical evidence was retained separately in the recovery workspace under `working/pi-oss-login-probe-2026-10-04/` (`receipt.json`, `verification.json`) and the earlier OSS/Pi probe. Those local probe records are not part of this repository. New component evidence is linked from [the operator guide](../../setup/oss-sandbox-lifecycle.md); it does not independently substantiate every historical probe claim.

## Approach and alternatives

Recommended: a local Python controller using the supported Singapore sandbox SDK, with a small Node runner using Pi 0.99.2. The Mac is an explicit POC runtime dependency. This avoids adding always-on compute and supplies a callable lifecycle boundary for the later gateway.

Alternative: implement the lifecycle inside a new web gateway now. This introduces hosting, request persistence and a queue before the first infrastructure case; defer it. Keeping separate throwaway scripts would require manual checkpoint selection and cleanup on every run, so it does not meet this integration's goal.

## Component boundaries

Proposed repository locations:

- `tools/sandbox_lifecycle/`: local CLI, strict nonsecret config, single-controller lock, durable run ledger, sandbox adapter and checkpoint metadata validation.
- `components/agent-harness/`: Node runner, Pi adapter, protected local state, checkpoint creation/restoration and bounded read-tool acceptance request.
- `tests/`: lifecycle failure cases, archive validation and Pi adapter tests with synthetic credentials.
- `docs/setup/`: operator usage, cloud acceptance receipt, retention and known limits.
- Dedicated offline CI checks; cloud credentials and real OAuth records are never CI inputs.

Use an isolated runtime dependency set with E2B 2.31.0, whose Singapore create/connect/mount path passed. Do not downgrade the existing AgenticFS bootstrap environment's E2B dependency. Pin the new dependency set during implementation. Pi stays at 0.99.2, Node at 22.23.3.

Configuration contains account, Singapore region, team, template, OSS bucket/prefix, Volume and execution-role identifiers. It also supplies limits and the selected model, initially restricted to the verified GPT-5.6 Luna. Account-specific values remain in ignored local config. No credential is a CLI argument or config value. Operator identity and sandbox API credentials stay outside the sandbox; Pi's OAuth record stays in sandbox local state and retained encrypted archives.

## Lifecycle

The local controller owns one protected ledger and exclusive filesystem lock for this account on this Mac. The ledger stores identifiers, state, checkpoint references and allowlisted diagnostics, never tokens, callback URLs or prompt/tool bodies. This is single-host coordination, not a distributed lock.

Commands are `status`, `run` and `recover`. They are proposed interfaces, not existing commands. `status` reads the ledger and verifies referenced resources without starting compute. `run` accepts a request file and limits, acquires the account lock and reconciles unfinished state before starting. A second invocation reports busy rather than creating another sandbox; an application queue is deferred.

Normal progression: idle → creating → restoring → running → checkpointing → terminating → finished. Errors preserve the last known sandbox ID and checkpoint reference. A request is successful only if Pi completed, the new checkpoint was verified and termination was confirmed. A model result may be complete while persistence or cleanup failed; those facts must remain separate.

Before create, record a unique attempt ID and pending-create state. Use SDK request correlation/idempotency only where actually supported. If create returns an uncertain outcome before an ID is recorded, stop with creation-unconfirmed; do not automatically create a replacement. Resolve ownership through API inventory where supported or operator reconciliation.

Create the proven Gen2 template with a 900-second sandbox timeout and the existing OSS Volume/scoped role. Install the verified pinned Node/Pi runtime for this first slice. The request defaults to a 90-second model limit, at most three assistant turns and 1,024 output tokens per model response. No unbounded automatic retries.

Restore credentials and sessions into a private local directory. Create a stable local host ID for this sandbox, excluded from portable checkpoints. Use the full saved OAuth record through Pi's own provider. Verify how Pi receives host identity during authentication; do not equate writing a host-ID file with sending it to OpenAI. No custom model-traffic proxy.

Run a new Pi session per request. Restored prior session files are retained, but a new request does not automatically resume old reasoning or replay operations. Deliberate continuation is a later interface.

After Pi settles, save the local auth record and session history, verify the checkpoint, record its reference, then terminate. On model failure, attempt to save changed credentials and settled session state before cleanup, while recording the request as failed. A failed or interrupted request is never automatically replayed.

## Checkpoint protocol

Use unique immutable archive names under a new lifecycle prefix inside the existing mounted bucket path. Do not overwrite the five retained probe generations. An archive contains only `auth.json` and `sessions/`; exclude host IDs, locks, request fixtures, temporary files, operator credentials and source code.

Create archives only when the runner is quiescent. Enforce local permissions 0700/0600. Reject archive entries with absolute paths, traversal, links or unexpected file types; impose a 16 MiB archive/expanded-state limit for this initial acceptance slice, with an explicit size-limit error rather than truncation.

Write archive and checksum through the OSS mount, then read them back and compare SHA256. Return only the immutable object reference, checksum, byte count and Pi version to the controller. The controller performs direct OSS metadata checks for object existence, expected size and AES256, using its existing local operator credentials without downloading the auth archive. Only after these checks does it atomically replace and fsync the local ledger's latest-checkpoint reference.

The local ledger is the first version's authoritative selector. Do not pick an object by numeric filename or modification time. A failure before ledger publication leaves the previous checkpoint selected. Unpublished archives remain retained for explicit inspection; never guess or silently adopt them. No OSS-mounted rename is assumed atomic. A remote latest-pointer service is deferred.

Bootstrap uses an explicit verified reference to the retained probe's checkpoint 5, supplied through ignored local configuration. Verify its metadata and checksum on sandbox restore before use. This is a deliberate one-time import, not a scan for whichever file happens to be newest.

The first version checkpoints after each request and during orderly failure cleanup, not after every internal OAuth refresh. A forced-refresh acceptance step checkpoints immediately afterward, as in the probe. A crash between provider rotation and checkpoint publication can still require signing in again; no crash-safe renewal guarantee is claimed.

## Interrupted runs and cleanup

`recover` operates under the same lock. If a known sandbox is still live, stop it and confirm not-found before admitting another run. If termination cannot be confirmed, keep the account blocked. If the old runner has already produced a complete checkpoint descriptor, verify it before advancing the ledger; never snapshot concurrently with a live Pi writer. Otherwise retain the previous reference and mark the request outcome unknown.

Creation uncertainty, unavailable ledger, malformed checkpoint, checksum failure and missing/denied storage stop before model execution. Terminal auth failure reports needs-sign-in with no raw provider response. This slice does not implement a replacement sign-in UI; the operator performs a separate explicit authorization flow if needed.

All temporary API keys are local-only, expire after at most one hour, and are disabled/deleted during cleanup. Keep their IDs in the ledger so recovery can retry cleanup. Confirm cleanup results rather than clearing unknown state. Retained bucket, role, Volume and credential archives are never deleted by run/recover.

## Acceptance and limits

Offline tests cover single-host exclusion, uncertain create, interrupted ledger writes, failed persistence, denied storage, corrupt/unsafe archives, size limits, unknown request outcome, failed termination and secret-safe reporting. Use synthetic credentials for injected failures. Pi adapter tests use the installed pinned package to establish auth/session API behavior without provider calls.

Live acceptance uses the retained credentials and existing storage: run one generated read-tool request, verify checkpoint publication and termination; replace the sandbox, force refresh, checkpoint and run; replace again and run without browser sign-in. Independently verify encryption metadata and cleanup. This tests the reusable component instead of reusing hard-coded A/B/C scripts as product code.

This design introduces no new persistent infrastructure. It retains storage/request charges and bounds temporary compute. It does not create a hard account billing cap or establish long-term unattended reliability. Distributed scheduling, continuous refresh checkpoints, checkpoint garbage collection, custom-template adoption, Raptor UI and Infra API operations remain subsequent slices.

## Primary references and self-review

- [OpenAI account/session guidance](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions): preserve full issued records, retain replacement refresh tokens and serialize renewal.
- [OpenAI VM guidance](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms): host identity and protected credential transfer.
- [Aliyun OSS mount guide](https://help.aliyun.com/zh/agent-sandbox/user-guide/mount-oss-volume): mounted object storage and sandbox SDK usage.
- Inspected Pi 0.99.2 `AuthStorage` and `ModelRuntime` interfaces; no credential file was read for interface inspection.

Self-review: this is one local-controller integration, with explicit interfaces, failure outcomes and acceptance. The publication selector, ownership scope and refresh-loss window are defined. No deployed queue, atomic FUSE operations, immediate refresh durability or custom image integration is asserted. The user approved this written design; the implementation plan requires its own review.
