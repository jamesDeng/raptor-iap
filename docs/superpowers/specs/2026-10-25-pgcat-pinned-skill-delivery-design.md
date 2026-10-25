# PgCat pinned skill delivery design

## Intent

The 2026-10-25 GPT-6 Luna replacement attempt skipped the pinned strategy's initial 2→4 expansion and called `request_pause` without an approval. The approval gate rejected the call and no scale action happened. Pi 1.3.0 omits its skills section when neither `read` nor `bash` is declared; this runtime intentionally declares neither. The selected immutable skill was staged but its instructions were not visible to the model. Deliver the verified pinned instructions without widening the agent's tools or repeating a paid test before review.

## Approaches

- **Selected:** Include the staged release's exact `pgcat-replacement/SKILL.md`, `references/contracts.md`, and `references/recovery.md` in the replacement system prompt. The files are an explicit allowlist, bounded and rejected if missing, changed to symlinks, or malformed. It costs prompt tokens but makes the same immutable release visible on the initial run and every resume.
- A scoped read tool would preserve Pi's normal skill loading but adds a tool and at least one model turn; the model could still skip the read in the ten-turn, 90-second window.
- Copying the strategy into the generic system prompt would be short but would decouple behavior from the immutable skills release and leave references unseen.

## Scope and safety

Keep `skills-v0.4.0` immutable and the request binding, selected SHA, `noTools: 'all'`, scoped MCP tool list, approval checks, 90-second/ten-turn limits and `replacementEnabled=false` release gate unchanged. Read only the three named files from the already verified staged release, with regular-file/no-symlink and total-byte checks. Fail closed before model inference if any file is unavailable. Qualification mode retains its current prompt and controls. Avoid logging the system prompt or private access material. The retrieved documents are instructions from the user-selected verified release; tool observations remain data.

## Verification and rollout

First add a failing fixture that captures the actual Pi model context and requires the strategy and both references while confirming `read` and `bash` are absent. Add malformed/missing-file fail-closed coverage, then implement the small prompt assembly change. Update both Gateway image packaging and the Gateway→AX guest transfer allowlist for the new runtime module; run their import-closure tests. Run the harness suite and affected Gateway tests, then review the diff. Publish immutable Gateway and AX Pi guest images from merged main: AX runs a baked `/opt/raptor-harness`, so a Gateway-only rollout leaves old prompt behavior in place. Pin both digests through their GitOps materials and verify both deployed images, flag false and idle Gateway. Before any new full replacement, complete a fresh read-only qualification, cost forecast and full traffic/fleet preflight. The failed request is terminal; create a new immutable request for a later run.
