# PgCat Generated Source Snapshot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep generated provider worker snippets bound to the same source bytes as the verified local module.

**Architecture:** The verified loader owns the source text used for Python introspection. Before executing each application module it installs that exact snapshot into `linecache` with no disk-mtime refresh, so `inspect.getsource` cannot reopen a different file. The source graph and runtime entry contract remain unchanged.

**Tech Stack:** Python standard library, Python 3.6-compatible loader, existing isolated subprocess tests.

**Spec:** `docs/superpowers/specs/2026-10-27-pgcat-no-scale-closure-design.md`, source execution contract and generated-source correction.

## Global Constraints

- No cloud, Workbench, credential or staging operations in this repair.
- Keep paid runtime entry and durable paid admission structurally disabled.
- Preserve verified module bytes, source locations, runtime guards, wrapper lock/deadline and all no-scale evidence rules.
- Do not change ordinary transition acceptance or discard capture frames.

## Review Focus

- A provider file changes after snapshot installation and is restored before the wrapper's next hash check: both generated snippets must still come from the original verified snapshot.
- A stale linecache entry exists before import: module execution must replace it with the verified text.
- Introspection occurs during module import: the verified text must be installed before `exec`, not afterward.

### Task 1: Bind introspection to the verified snapshot

**Files:** Modify `test-assets/traffic/pgcat_source_bootstrap.py`; test `test-assets/traffic/test_pgcat_source_bootstrap.py`.

**Interface:** `VerifiedFinder.exec_module(module)` installs its opened source bytes into a loader-owned `linecache` record keyed by the exact compile filename before compiling/executing those bytes. Python introspection for that module reads the same snapshot; it does not revalidate against current disk mtime. No generic execution API is added.

- [ ] Add a failing isolated-root regression that changes `provider_feed.py` after `install` but before import, restores disk before the next wrapper hash check, and proves both `_ATOMIC_SOURCE` and `_PRIVATE_PUBLICATION_SOURCE` currently differ from the verified module snapshot. Include a preexisting incorrect linecache entry.
- [ ] Run the focused regression and record the actual RED output.
- [ ] Install the exact decoded source lines with a non-refreshing snapshot cache entry before module execution; preserve filename and verified hash checks.
- [ ] Run focused normal/optimized tests and the full traffic normal/optimized suites serially; run Python 3.6 grammar and diff checks.
- [ ] Commit only the two scoped files, append a report with commands/output, and obtain a focused independent review before source integration.
