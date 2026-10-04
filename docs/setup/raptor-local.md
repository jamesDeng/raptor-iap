# Local Raptor application questions

Status: two real browser-submitted questions passed on October 4, 2026. See [the sanitized acceptance receipt](raptor-local-acceptance.json). Both used the real scoped context tool, retained private answers, saved checkpoints and confirmed cleanup. Creation/confirmed termination times establish serialization; the SDK sandbox list missed a known running sandbox and is not used as that proof. Answers mixed AWS ECS terminology into Aliyun ECS discussion, so owner review remains necessary.

This is a single-owner local POC: a loopback web process persists app context and tasks, and a separate gateway processes one task at a time using the existing Singapore lifecycle. Each question gets a fresh Pi reasoning session and an immutable context snapshot. Only the registered context tool is available. There are no infrastructure mutation tools yet.

## Preparation and launch

Use Python 3.12 and Node 22.23.3. Follow the [lifecycle preparation](oss-sandbox-lifecycle.md) for the pinned dependencies, private config, retained encrypted checkpoint and existing Aliyun `infra-ops-poc` profile. Pi is pinned to 0.99.2 and the sole enabled model is `gpt-5.6-luna`. Authentication uses the previously authorized ChatGPT Pro account through Pi's Codex provider; it does not use OpenAI API credits. Availability and account limits still apply.

From the repository root, in two separate terminals:

```sh
.raptor-local/lifecycle-runtime/bin/python -m components.raptor.server
```

```sh
.raptor-local/lifecycle-runtime/bin/python components/agent-gateway/worker.py \
  --config .raptor-local/lifecycle-config.json --profile infra-ops-poc
```

Open **http://127.0.0.1:8765**. Use that exact address; `localhost` is deliberately not an alias. Register nonsecret context, mark each dependency planned/present/unknown, and submit a question. Leave unknown identifiers blank. These labels are owner declarations, not health checks. Do not put connection passwords, OAuth tokens, API keys or private keys in context or questions. Obvious credential patterns in app metadata are rejected; validation is not a universal secret detector.

Each request is limited to 2,000 Unicode characters/8 KiB, context to 32 KiB and ten dependencies, and the answer to 16 KiB. Inference is bounded to 90 seconds, three assistant turns and 1,024 output tokens per response; sandbox lifetime is 900 seconds. There is no automatic whole-question retry or model fallback. The model must actually read the bound context tool for a contextual answer to succeed.

The page polls persisted progress every two seconds. Closing it does not stop the separate worker. Editing an app affects new questions; existing tasks keep their saved version. Task history shows requested/actual model, context hash/tool evidence, usage and attempts. Tool use and a completed answer do not prove every statement is correct; missing information and owner declarations need human interpretation.

## Outcomes and recovery

Answer generation, checkpoint publication and cleanup have separate outcomes. A visible completed answer with a failed checkpoint or cleanup remains a failed/blocked task. Unknown outcomes remain unknown. Queued tasks can be cancelled before compute starts. Active-task cancellation is deferred.

After interruption, restart the worker and inspect the task. It blocks dispatch and never automatically replays a claimed question. **Recover interrupted run** requests cleanup/adoption of a known completed, correctly bound result; recovery itself does not send the question again. **Retry as a new attempt** becomes available only for a failed task after cleanup is confirmed and dispatch is unblocked. It deliberately repeats the original question/context and retains the earlier attempt.

If ownership, sandbox creation or cleanup cannot be established, retain the database and lifecycle ledger. There is no force-unlock or delete-ledger button. Resolve the recorded pending intent using the [lifecycle recovery guidance](oss-sandbox-lifecycle.md#failure-and-recovery); do not use another clone or host to bypass the lock. A worker holds a process lock, and all lifecycle entry points share the canonical account lock in one checkout family. This is not distributed coordination.

SIGTERM/SIGINT asks the worker to finish its current bounded operation and then stop. `--once` processes at most one task/recovery action. A hard process kill may leave a blocked attempt that needs explicit recovery. A failed worker prints only a fixed error; private task details remain in local storage.

## Private state and access boundary

Linked worktrees share the primary checkout's ignored `.raptor-local/`:

- `raptor/raptor.sqlite3` and SQLite sidecars: owner context, questions, immutable snapshots, private answers and task history.
- `raptor/request-token`: local browser mutation token; `raptor/worker.lock`: worker exclusion.
- `sandbox-lifecycle/<account-id>/ledger.json`: safe lifecycle metadata, ownership and checkpoint selection.
- `sandbox-lifecycle/<account-id>/task-results/<task-id>/<attempt-id>.json`: protected bound private result artifacts.

Directories use 0700 and files use 0600; symlink paths are rejected. Do not commit, publish, email or attach these files as public diagnostics. No complete auth archive or Pi session log is downloaded to obtain an answer. Raw provider errors are suppressed. The web process does not acquire cloud operator credentials, and those credentials never enter browser tasks or sandbox job files.

The server binds only loopback, checks exact Host/Origin and a local mutation token, rejects cross-site reads, and renders answers as text. It provides no remote access, multi-user login, permissions model or production hosting. Local processes under the same OS user can access private state; this is not an isolation boundary against that user.

## Checkpoint limitation and budget

OSS credentials are stored as encrypted checkpoints, restored to sandbox-local files. OSS-mounted rename is not claimed atomic. Internal OAuth refresh followed by a crash before checkpoint publication can lose the new refresh token and require sign-in again. AgenticFS/NAS mounting and automatic unselected-checkpoint adoption/garbage collection remain deferred. Retained OSS storage remains chargeable.

Before real questions, inspect cumulative billing and retained resources, estimate bounded sandbox runtime, and keep the POC below RMB1,000. This application does not enforce an account spending cap. Offline checks use synthetic cloud/model transport; live evidence, when recorded, is separately identified in the acceptance receipt. The dedicated test app and zero-failed-operations PgCat replacement acceptance are not implemented by this slice.
