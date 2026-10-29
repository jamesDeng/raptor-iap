# Task 3 — explicit direct provider/capture transition mode

Status: implementation and verification complete; ready for parent independent review. Review base: `9ab90d161e4cdc112cb31a26a810e959cab5f811` (Task 2 native fix, disjoint source paths). Initial Task 3 base was `88eeb826ad9c8126ae47b71db602ddef9bf69865`; work paused at the parent's serialization checkpoint and resumed unchanged after the native fix. No whole implementation plan or other task ledger was consulted.

## Scope and behavior

Owned source paths:

- `test-assets/traffic/provider_feed.py`
- `test-assets/traffic/capture_window.py`
- `test-assets/traffic/test_direct_replacement_mode.py` (new)
- `test-assets/traffic/README.md`

The producer CLI and inventory sanitizer recognize `full-replacement-direct-v2`. Producer and assessor share the direct `InventoryMode` state machine. It binds an exact two-original baseline, permits desired-four expansion with asynchronous physical membership, binds the exact added pair from the complete four-member roster, then admits one desired-four→two action with two original departures. Four/three physical members may persist at desired two. Originals cannot leave early or reappear, replacements cannot leave, desired three/re-expansion/late or extra identities are rejected, and completion requires exactly the added pair as membership/backends/healthy registered IDs. Session/sequence continuity remains required by the retained assessor, with explicit incoming producer sequence drift rejected for the new mode.

The direct drop grants two departure slots once; this does not create two independent scale actions. `Operator.read()` still uses authoritative snapshot validation, and `write()` validates topology before changing targets or publishing. All learned scrape targets remain retained through original departure and evidence retirement. Existing freshness, address coverage, deadlines, raw triplets, SQL failure precedence and zero-plus-subsequent-fresh-backend-absence logic are unchanged.

Self-review found that the generic assessor could complete while old-node nonzero metrics stayed fully covered. The parent ruled that **new direct-mode completion must prove both originals currently retired**; historical full replacement and disabled rehearsal completion remain unchanged. Three explicit RED subcases established the gap (both nonzero, only one retired, later nonzero after retirement). The minimal direct completion predicate now receives the existing `retired` set and requires both originals in it. Existing later-nonzero/re-registration behavior invalidates that proof and restores coverage obligations. This component does not infer actual mutation chronology from provider timestamps; the future native observer must join action evidence without invented cross-host clock guarantees.

Runtime admission is closed: CLI arguments still require the existing run context, and `Operator` still accepts that context only for `disabled-rehearsal`. Direct mode is parsed but refuses startup before provider commands/publication. No receipt validator, lane, worker, collector, bootstrap, release guard, native harness, approval or deployment path changed. Unbound `Operator` fixtures exercise the topology library; they establish no live qualification or runtime enablement.

## Verification commands and retained outputs

All commands ran from this existing checkout with standard local `python3`; no cloud, network, credential or billing operation was performed. Logs are under `.superpowers/sdd/2026-10-29-pgcat-direct-replacement/logs/` and remain local ignored artifacts.

1. Initial feature RED:
   `python3 -m unittest discover -s test-assets/traffic -p 'test_direct_replacement_mode.py'`
   → 7 tests, 2 assertion failures and 3 missing-mode constructor errors; exit 1.
   Log: `task-3-red.log`. The errors came from the absent new mode; a subsequent narrowed positive fixture established a clean assertion RED before production edits.
2. Clean focused RED:
   `python3 -m unittest discover -s test-assets/traffic -p 'test_direct_replacement_mode.py' -k direct_sequence`
   → 1 test, 1 assertion failure (`INCONCLUSIVE` rather than expected `PASS`, incomplete provider inventory); exit 1.
   Log: `task-3-focused-red.log`.
3. First minimal implementation GREEN:
   `python3 -m unittest discover -s test-assets/traffic -p 'test_direct_replacement_mode.py'`
   → 7 tests OK; exit 0. Log: `task-3-green-attempt-1.log`.
4. Expanded authoritative read/write, CLI and compatibility fixtures:
   same direct test command → 11 tests, 1 failure (`missing metrics coverage`); exit 1.
   Log: `task-3-extended-focused.log`. The synthetic provider fixture incorrectly retained its default Protected lifecycle for Pending additions. Correcting the fixture to preserve the published lifecycle, without changing production behavior or timing, produced 11 tests OK; exit 0.
   Log: `task-3-extended-focused-fixed-fixture.log`.
5. Added uncovered backend-identity case:
   same direct test command → 11 tests OK; exit 0. Log: `task-3-final-focused.log`.
6. Existing disabled rehearsal compatibility:
   `python3 -m unittest discover -s test-assets/traffic -p 'test_rehearsal_mode.py'`
   → 16 tests OK; exit 0. Log: `task-3-rehearsal-focused.log`.
7. Parent-directed direct completion RED:
   `python3 -m unittest discover -s test-assets/traffic -p 'test_direct_replacement_mode.py' -k completion_requires`
   → 1 test with 3 failing subcases, each incorrectly returning PASS with fully covered old metrics; exit 1.
   Log: `task-3-retirement-red.log`.
8. Direct completion GREEN:
   `python3 -m unittest discover -s test-assets/traffic -p 'test_direct_replacement_mode.py'`
   → 12 tests OK; exit 0. Log: `task-3-retirement-green.log`.
9. Frozen full normal suite, exactly once:
   `python3 -m unittest discover -s test-assets/traffic -p 'test_*.py'`
   → 825 tests OK in 65.725 seconds; exit 0. Log: `task-3-full-normal.log`.
   The known `optional timing output unavailable` diagnostic appeared twice; no test failed or skipped.
10. Frozen full optimized suite, exactly once:
    `python3 -O -m unittest discover -s test-assets/traffic -p 'test_*.py'`
    → 825 tests OK in 64.931 seconds; exit 0. Log: `task-3-full-optimized.log`.
    The same known optional timing diagnostic appeared twice; no test failed or skipped.

No full suite ran concurrently with another suite, no full-suite retry was made, and no timing/freshness limit was widened.

## Static verification, self-review and limits

Before freeze and again after both full suites (log `task-3-static.log`), `ast.parse(path.read_text(), filename=str(path), feature_version=(3,6))` passed for the two changed production modules and the new test module. `git diff --check` passed, and a SHA-256 comparison confirmed all four frozen files unchanged after both suites. Frozen source hashes:

- provider_feed.py: `67da2a19d1fa340d008a5fbcee4d5df4d50bfb3d8e8e40f41ab06a42255b788e`
- capture_window.py: `1db16197ba17d87b7a1a909a5b66ab811f6942665b24775a2d692e4f4a75ec68`
- test_direct_replacement_mode.py: `bed2cc64c762ddfb3a27eea2ea4da54173e1bfe6b4e3d0339f606869d3a0dee2`
- README.md: `33d8f5c74c7255c089a34e47d864b1f4300b7dbb41c3350aa82b109384f54064`

Self-review traced baseline/expansion/drop phases, monotonic original departure, lost/foreign replacement refusal before publication, historical mode branches, independent old-node zero/absence evidence, target retention, sanitizer allowlisting, and native runtime startup refusal. The test fixture exercises authoritative snapshot → producer publication → retained assessment and segmented archive reassessment. No broader source refactor or new component deployment was included.

Unrelated untracked iCloud duplicate files were left untouched. Source changes are restricted to the four owned paths above; this report is the only task ledger added. Parent independent review remains required. Local fixture PASS does not mean live PASS. Exact provider/action timestamp joining, observer-profile integration, rollout binding and deployment remain future separately reviewed work.

Commit: implementation/test/docs and this report committed together; identify the commit by `git log -1 --format=%H -- test-assets/traffic/test_direct_replacement_mode.py`.
