# Minimal Raptor and gateway — app-context questions

Status: architectural design approved by the user's “go ahead” on October 4, 2026, following written review. Implementation planning is authorized; no Raptor/gateway product code, dependencies or cloud resources are introduced by this document.

## Agreed intent and current evidence

Build a minimal Raptor from scratch; no Raptor source code is available. Raptor should register applications and environment context, accept standalone agent tasks, and display progress/results. The user chose natural-language app questions before GitOps PR creation. Clarification messages belong to tasks, rather than a general-purpose chat product. Start locally, reuse the real Aliyun Singapore sandbox lifecycle, and retain one active Pi run for the shared renewable account.

The eventual POC remains agent-driven PostgreSQL RDS deployment, PgCat on ECS/ESS deployment, and node replacement with zero failed application operations. ACK and Argo CD are future environment prerequisites. This slice establishes the request path and app context; it does not implement those infrastructure cases or claim their resources exist.

Implemented baseline: merged PR #3 contains the Python sandbox lifecycle, Node/Pi runner, encrypted OSS checkpoints and recovery/cleanup. Its inference request is a generated read-probe, with boolean verification output. It currently accepts neither arbitrary user questions nor app context, and does not return a natural-language answer to a Raptor UI. Those are explicit extensions in this design.

Latest model/auth decisions: Pi 0.99.2, Node 22.23.3, GPT-5.6 Luna through the retained ChatGPT Pro OAuth session; Pi calls OpenAI directly. The gateway does not proxy model traffic. The prior internal-refresh-before-checkpoint crash limitation remains accepted. Total Aliyun POC spend must remain below RMB1,000; no new always-on cloud resources are proposed.

## Approaches and selected direction

1. **Recommended: local Raptor service and separate local gateway worker.** Raptor serves the UI and stores app/task records; a worker consumes the durable queue and calls the existing lifecycle. Browser closure does not stop tasks. Distinct components can move to the future platform without duplicating sandbox logic. The cost is managing two local processes and their persisted handoff.
2. **One service with an in-process worker.** Fewer process boundaries, but a web-service restart also stops its worker and couples HTTP handling to cloud execution.
3. **Host Raptor/gateway on ACK immediately.** Closer to the eventual environment, but requires the unbuilt ACK/GitOps foundation, deployment identities and additional running costs before app-question behavior is proven.

Use option 1. Preserve component folders in the single public repository. Keep app/task data and account configuration in ignored local storage. The first UI is single-owner and local; owner metadata is not an authentication or authorization system.

## Raptor experience and app context

Provide an app list, app detail page, new-task form and task detail/history. Register and edit the dedicated `db-client` app manually. Other manually registered apps may use the same schema, but each task is scoped to one selected app.

App fields: stable app ID, name, owner label, environment name, region, optional ACK cluster identifier, namespace, deployment name, and dependencies. Each dependency has a name, type (`postgres`, `pgcat` or `other`), lifecycle label (`planned`, `present` or `unknown`), and short nonsecret notes. Blank deployment/resource identifiers remain unknown; registration must not invent them.

Raptor stores a version and update timestamp for the app record. This first catalog is owner-entered information, not live discovery. The app page and agent-visible snapshot identify that provenance. An owner-entered `present` label is a declaration, not independent verification of health or deployment. The initial db-client/RDS/PgCat/ACK records are planned unless the owner supplies actual information. No credentials, connection strings, kubeconfig or provider tokens belong in these fields.

The form accepts a question and the selected app. Show GPT-5.6 Luna as the sole enabled model; retain requested/actual model identity without offering unverified alternatives or silently substituting. Do not add app deployment, scaling or restart actions in this slice. The real restart action follows once ACK and the test app are available.

Example question: “What infrastructure does db-client need, and what is still missing?” A useful answer cites the registered snapshot, identifies planned/unknown information, and avoids claiming live inspection occurred.

## Request path and ownership

1. Raptor validates the selected app and question, then transactionally stores the immutable question, model, app snapshot/version, submission time and task ID together with the queue entry.
2. The gateway claims one queued task using a durable transaction and holds a single local worker lock. Additional requests remain queued. Existing lifecycle account locking remains the final guard against overlap with direct CLI use.
3. The gateway starts an `app-question` request through the existing lifecycle, retaining its canonical account ledger, checkpoints, fixed limits and cleanup behavior.
4. Pi receives the question and accesses the saved app snapshot through one custom context tool. It returns an answer and bounded evidence describing which snapshot it used.
5. After inference settles, the existing lifecycle checkpoints and confirms cleanup. The gateway stores the private answer and safe run evidence; Raptor displays them on the task page.

The worker runs independently of browser requests. Closing or refreshing a page must not lose the task. Running tasks keep the snapshot taken at submission even if the app is edited while the task is queued. A new question uses a new snapshot and task; deliberate continuation/resumption of a Pi reasoning session is deferred.

Suggested component placement: `components/raptor/` for UI/API; `components/agent-gateway/` for the worker and task orchestration; a small shared Python task-store module for persisted contracts. Reuse `tools/sandbox_lifecycle/` and `components/agent-harness/` rather than creating another cloud adapter. Use local SQLite for atomic app/task/queue transitions and a small Python web service with a browser UI; concrete package pins are determined and checked during implementation planning. No Redis, message broker or managed database is needed for this single-owner local slice.

## Agent capability and answer boundary

Add a separate `app-question` kind; preserve the existing `read-probe` acceptance path. A question is UTF-8 text of at most 2,000 characters/8 KiB. A validated app snapshot is at most 32 KiB, with no more than ten dependencies. Reject unknown schema fields. Do not interpolate questions or context into shell commands.

Use the pinned Pi SDK's `customTools` and explicit tool allowlist. Expose a single tool provisionally named `get_application_context`, bound to this task's saved snapshot. It accepts no filesystem path, URL, shell command or arbitrary app identifier. Exclude all built-in filesystem, shell and editing tools; extensions, skills and automatic context discovery stay disabled for this acceptance slice. The implementation must verify the actual registered/enabled tool set against the real SDK, not rely on prompt instructions for the restriction.

The agent may reason and phrase its answer freely. There is no scripted deployment plan or canned answer. The system context explains owner-entered provenance and unknown facts, and requires using the context tool for app-specific claims. Tool output is app data, not an authorization source. A missing or failed context read must not produce a successful contextual task.

Existing bounds remain: sandbox lifetime 900 seconds, model timeout 90 seconds, maximum three assistant turns and 1,024 output tokens per response, no automatic model or whole-request retry. An empty answer, aborted/error response or exceeded limit fails the answer operation. Successful answer generation does not establish correctness of all model claims; UI evidence separately identifies actual context-tool execution, model identity and token usage.

Natural-language text is an intended private task result, not a public diagnostic field. Preserve the current credential-free lifecycle status/report boundary: allowlisted metadata and references in the canonical ledger, no user question, app snapshot, model answer or raw provider error. Store a bounded answer (at most 16 KiB) in protected ignored local task-result storage, bind its reference/hash to task and attempt IDs, and store it with task history only after validation. Do not fetch auth archives or entire Pi session logs onto the Mac to retrieve an answer. Recovery may read the specific completed runner-result payload needed for that task.

Render answers and submitted text as escaped text; model output is not executable HTML. Raw OAuth/provider errors remain fixed safe codes. No question, private answer or real app data is committed to Git or placed in CI receipts. Synthetic data alone is used in offline tests.

## Progress, failures and recovery

Show task status plus the latest known stage: queued, starting, restoring credentials, asking the agent, saving checkpoint, cleaning up and finished. First version uses periodic UI polling of persisted events. Live token streaming is deferred. Events include task/run identity and timestamps, not raw model/provider logs.

Keep three outcomes separate: answer generation, checkpoint publication and sandbox/control-key cleanup. A generated answer may be displayed with an explicit persistence/cleanup failure; it must not make the overall run appear fully successful. When the runner outcome is unknown, report unknown rather than guessing completion.

An interrupted worker never automatically replays a claimed question. On restart, reconcile its durable task/run ownership with the canonical lifecycle ledger, recover known resources and only adopt a complete, correctly bound result. If mapping, creation, persistence or cleanup is uncertain, block dispatch and retain the affected task and IDs for operator recovery. A second worker must not dispatch another task simply because a local time limit or lease expired. A failed task remains failed until an explicit user retry creates a new attempt with clear history.

The task page offers a narrowly scoped recovery action for blocked lifecycle state, using the existing recovery boundary. It does not offer delete-ledger/force-unlock controls. Unclaimed queued tasks can be cancelled without compute. Active-task cancellation, interactive in-sandbox approval pauses and resumable clarification exchanges are deferred; the task can state missing information, and the owner may submit a follow-up as a new standalone task. No deployed pause/resume feature is implied by the later platform requirement for task-linked clarifications.

Serve only on loopback in this POC. Protect state-changing web requests against cross-origin access and host rebinding, using exact host/origin checks and a local request token. Cloud operator credentials remain private to the local controller/worker and are never browser inputs or sandbox job fields. This is not a public multi-user service or a production permission model.

## Acceptance and limits

Offline acceptance must demonstrate real persistence across web/worker restart; queue exclusion between workers and direct lifecycle invocation; immutable app snapshots; validation and task/result binding; no replay after interruption; and truthful rendering of answer, checkpoint and cleanup failures. Exercise every failure with synthetic data.

Use real pinned Pi session APIs with only the remote model boundary replaced to verify the tool allowlist, actual context-tool invocation, new session per task, bounds, and answer isolation. An adversarial question asking to read `auth.json`, run a command or change cloud resources must find no such tool available. Test that another app/task cannot be substituted through tool arguments, result references or API routes.

Positive live acceptance after implementation approval: create a nonsecret planned db-client record through Raptor; submit a natural-language question in the browser; observe it reach real Pi in a Singapore sandbox; verify the saved snapshot and context-tool evidence; display its answer; verify checkpoint publication and confirmed compute/key cleanup. Queue a second question and establish that only one sandbox is active and that its result is associated with the correct task. Preserve a sanitized receipt without questions, credentials, real private app data or complete answers.

Before paid tests, check remaining cumulative budget and estimated bounded runtime using available billing/resource information. This design does not introduce a hard account spending cap or authorize leaving persistent cloud compute running. Existing OSS storage/checkpoints stay retained; no automatic deletion is introduced.

Success proves Raptor → gateway → sandbox → Pi → visible app-context answer. It does not prove live infrastructure discovery, PR creation, deployment, app restart, infrastructure permissions, multi-host coordination, long-term OAuth crash safety or zero failed application operations during replacement.

## Provenance and self-review

The current conversation approved building minimal Raptor from scratch, local services, standalone app questions and the pinned runtime/model/auth. The implemented baseline is [the merged lifecycle guide](../../setup/oss-sandbox-lifecycle.md), with its acceptance/review records. Repository source under tools/sandbox_lifecycle and components/agent-harness was inspected. Earlier recovery and wiki documents informed the architecture but do not prove prior Raptor deployment or authorize historical commands.

App questions, declared context, task ownership, private output, scoped tools and queue/recovery are new capabilities in this slice. Restart, approval pauses and GitOps remain future work.
