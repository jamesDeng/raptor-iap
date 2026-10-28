# PgCat Fixed Post-stop Lane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax for tracking.

**Goal:** Admit one real journal-derived post-stop finish without reopening ordinary live-feed calls.

**Architecture:** A dedicated client/supervisor path and one source-verified wrapper role consume a semantic one-use journal admission. Frozen receipt identity and actual original stop provenance replace the unavailable live publication; successful returned boundary still must meet original5s/finalage30. No public paid release or final verdict is supplied.

**Tech Stack:** Local Python3.9, Python3.6-compatible shared source, private SQLite/JSON/source snapshot loader and fake CLI fixtures.

**Spec:** `docs/superpowers/specs/2026-10-28-pgcat-post-stop-lane-design.md`

## Global Constraints

- Existing linked checkout/branch only; no cloud/network/credentials/billing/staging/Raptor/deployment/push in implementation.
- Keep both NO_SCALE_ACTIONS, REQUEST_KEYS, BINDING_KEYS, source-map names and production _release_admission bodies unchanged. Add only rehearsal-finish general action/post-stop fixed role.
- Normal provider/external freshness/intent/argv/lock behavior stays unchanged; no generic command, role-env authority, live successor, receipt retouch or clock anchor.
- One actual source/run/owner/stop-derived semantic admission before Popen and one wrapper dispatch claim under inherited stable flock before exec.
- One original host-wide Mac5s deadline; actual final receipt age<30 before dispatch AND on result/seal; pre-call age<24 for headroom. No resetting budget or claim of remote cancellation.
- Worker result is exact six-key PREPARED boundary summary, never ordinary PASS. Unknown first attempt consumes claims and cannot replay under anotherID.
- Successful stop/finalized duplicate or ordinary call refuses pre-dispatch while retaining actual owner/proof; first uncertain attempt closes under existing semantics.

## Review Focus

- Direct J/role-env caller cannot substitute a cached dict or forged argv for actual bound stop/source/exit/admission (Task1).
- Same JSON in a new receipt inode or changed original/current manifest is refused (Task1).
- A final publication already consumed by ordinary intent still permits only the one semantic finish; concurrent newIDs never replay (Task1).
- Late result or provider age30 cannot seal success despite plausible worker hashes (Task1).
- Ordinary/duplicate calls after finalized clients cannot destroy the owner needed by future closure (Task1).

### Task1: Dedicated journal/client/supervisor/wrapper admission

**Files:** Modify `test-assets/traffic/pgcat_lane_protocol.py`, `pgcat_lane_actions.py`, `pgcat_lane_client.py`, `pgcat_lane_supervisor.py`, `pgcat_workbench_lane.py`, `pgcat_rehearsal_controller.py`; matching six fixture modules. No worker/provider/collector/bootstrap changes without concrete dependency report. Start only after previous exact worker source review.

**Interfaces:** Spec pins table/admission/binding shapes and fixed action. Add pure current-stop derivation helper reused by record_rehearsal_stop and context admission, preserving stored stop shape. `Journal.post_stop_context(request)` derives worker six-key context; `post_stop_admission(request_id)` read-validates and returns exactly `{request,admission,bindings}`; `post_stop_dispatch(request_id)` atomically claims and returns the same shape. Admission has exact eight fields from spec plus separate dispatch_claimed column. The getter must verify stored request hash/current independently derived stop/actual mode/source before returning bindings. `Journal.post_stop_binding()` returns exact sealed eight-field binding. Journal.seal private post_stop_source_sha only for this fixed complete action. Existing methods default unchanged.

`ActionCatalog.post_stop_worker_command(bindings,run_id,context)` generates fixed source-verified worker command using existing11-key collector union; command private lane_root/request_id kwargs only on rehearsal-finish returns rolepost-stop and budget5.0. Fixed parser verifies exact two JSON lines/six-key payload/session/sequence/source. Wrapper new role checks verified-source loading, actual J admitted state/context/original bindings, exact remote argv/realCLI/lock, original req deadline; under same inherited flock revalidates then claims dispatch before os.execv, no publication consume_intent. Role env alone refuses. Existing branches unchanged.

`LaneClient.post_stop_call()` no args derives current actual stop/request and waits on J under original deadline, no live receipt/successor. Supervisor dedicated private dispatch path runs before ordinary receipt/unused checks, admits before actual spawn under mask, retains same process lifecycle, source/owner/currentstop/finalage/deadline before dispatch and on result/seal. `Controller.finish_rehearsal_clients()` onlyfeed-stopped actual retained feedexit0, validates Jbinding/result, storesproof/entersclients-finalized, keepsowner. Extend stopped-state refusal across both states; duplicate/directordinary calls preserve authenticated owner, no generic post-exit call.

- [ ] Add RED tests for actual original stop derivation/no zero exit/no immutable manifest/wrong collector/mode/source; same-byte new inode/stale receipt; one semantic claim/concurrentIDs even consumed oldpublication; direct role-env/argv/root/CLI/lock/admission forgery; fixed actual same-PID inherited flock exec and timeout consumed claim; noarg client/supervisor exact timely result/newstate/owner retained; original5s or finalage30 refusal; fake seal/context/source/result; ordinary/duplicate refusal with owner/proof retained. Existing source-release tests may patch guards only in test code; no runtime test/env enable flag.
- [ ] Run RED focused fixtures, retain exact feature-related failing output.
- [ ] Implement only fixed path and independently derived admission/result shapes. Preserve source guards/defaults, actual subprocess provenance, masked admission and original deadline across every step. No independent PASS/finish cleanup.
- [ ] Run focused normal/O then full traffic normal/O sequentially on final source, Python3.6 grammar, protected constant/function AST comparison and diff checks. Diagnose failures, no timing widening/skip/retry-to-green.
- [ ] Self-review, commit only owned12files, report exact RED/GREEN/interfaces/limits and obtain independent scoped review. Public paid release remains closed.

## Following gates

Same-handle guardian abort/actual exit, durable authenticated FAIL/ordinary detached verdict, actual2/2 restoration/identity-bound credential cleanup/independent finish, adequate closure headroom/cutoff, full native observer and session handoff remain pending. All common source changes must precede final freeze/no-scale long proof/paid rehearsal. No cloud operation is part of this plan.
