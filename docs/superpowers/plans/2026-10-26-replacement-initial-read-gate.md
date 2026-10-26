# Replacement initial read gate plan

1. Add a failing synthetic harness test for first-turn read-only tool exposure, valid fleet baseline unlock on the next turn, and no unlock for invalid fleet evidence.
2. Update `runLiveReplacement` to select the initial active catalog from the validated replacement trace and expand it only after a completed round with a persisted baseline. Add a concise initial-read instruction.
3. Run focused and whole harness tests, inspect the diff and independent review findings, then commit and create a PR. Merge only after checks and review.
4. Publish/pin the reviewed image through the existing GitOps flow. Recheck the disabled-mode state, exact worker capture and fleet before a new replacement request; keep the user’s cost and 21:30/22:30 Shanghai cutoffs.
