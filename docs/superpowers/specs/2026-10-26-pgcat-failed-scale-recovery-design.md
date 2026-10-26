# PgCat failed 2→4 attempt: return to the protected baseline

## Intent and observed state

Return the rdev.ali PgCat test fleet to its two protected originals after request
`570533ed-e417-4bc5-beba-ad4db3db8d20` failed before either scale-in approval.
This is cost and traffic recovery. It cannot make that attempt a replacement PASS.
Raptor reports a verified checkpoint and confirmed cleanup. The Gateway's guarded
disable receipt verifies `replacementEnabled=false` (Secret RV `10959096621`,
generation 154), and a separate idle check returned `idle=true`.

At 12:45 Shanghai on 2026-10-26, authoritative ESS and NLB reads showed desired
four and four Healthy/Normal members. Originals `i-t4n8ofwrovy3mv198ela` and
`i-t4n8ofwrovy3mv198elb` were Protected. New nodes
`i-t4n237xiu1dr1lkxfypk` and `i-t4n237xiu1dr1lkxfypl` were unprotected.
The two new nodes each had three **idle** PgCat connections in a fresh metric
sample. The test clients and metrics Deployments were 2/2 and 1/1 Ready.

## Approaches

1. Leave all four healthy nodes until the scheduled shutdown. This avoids a
   recovery mutation, but retains two extra instances and their cost and does
   not restore the expected two-node baseline.
2. Scale directly from four to two. Reject: it bypasses PgCat's zero-client
   gate while the new nodes hold connections, and can breach the strict
   healthy-majority condition during drain.
3. **Preferred:** staged 4→3→2 recovery, with only the two new nodes removed,
   fresh checks before each action, and no automatic retry of uncertain calls.
   This uses existing ESS/NLB runtime APIs, not cloud resource provisioning.

## Staged recovery contract

Recheck the exact request is terminal with cleanup confirmed, no pending
approval or active Gateway work, flag false/idle, sole operator, exact group,
server group and four identities, both originals Protected, both new nodes
unprotected, healthy clients/SQL, complete fresh metrics and all four healthy
NLB backends. If any precondition changes, stop without mutation.

Keep `...xfypl` temporarily Protected to constrain ESS's first candidate.
Deregister only `...xfypk` from NLB while three healthy backends remain,
restart only the two GitOps-owned test clients through the existing bounded
runtime operation, and verify restored client identities, successful SQL and
fresh zero active/idle/waiting PgCat connections for `...xfypk` across every
pool/user. Submit one ESS desired-capacity change 4→3 with retries disabled.
Verify its provider acknowledgement separately from full convergence: desired
three, `...xfypk` absent from ESS/NLB, and both originals plus `...xfypl`
healthy and registered. A missing acknowledgement or uncertain inventory stops
recovery; never replay the action.

Then unprotect only `...xfypl`, deregister only it while two healthy backends
remain (strict majority at desired three), restart/rebind the two test clients,
verify successful SQL and fresh zero connections on that node, and submit one
non-retried 3→2 change. Verify desired two, exact two protected originals,
healthy NLB registration, test clients 2/2, fresh metrics/SQL, Gateway false
and idle. A failed gate stops at the actual observed fleet; it never triggers
an automatic reverse action.

After the single full-window capture and provider feed are closed, reconcile
the `raptor-test/pgcat-provider-inventory` ConfigMap to the freshly verified
actual fleet with its current resourceVersion, then read back exact target IDs.
Preserve the failed-run private evidence and its SHA. Remove both exact
temporary kubeconfig copies only after this recovery and final evidence checks;
do not issue a new credential or expose credential material. Record every
runtime request ID, before/after state and any unresolved step privately.

The prior run's collector is `INCONCLUSIVE` for missing metrics coverage and
cannot be upgraded. A new 2→4→3→2 attempt requires a new continuous capture,
fresh preflight and separate request-bound approvals for its two scale-ins.
