# Resume and uncertain outcomes

Read the durable request/attempt journal and recorded logical operation IDs. Re-discover the group, DB association, current nodes/protection, registered/healthy targets, desired capacity, Deployment UIDs and source metrics. If topology or DB association changed, pause for a fresh plan; do not assume the original node still belongs to this request.

Pending/unknown scale or partial node command: use read-only operation/provider status. Do not issue another mutation or automatic rollback until the authoritative outcome and membership are reconciled. A consumed approval is not authority for another logical scale-in. Retry semantics for the same operation must come from the reviewed server contract; the skill cannot reset consumption or mint new approval.

Restart failure, stale/missing metrics, rejected majority check, denied approval, unavailable evidence collector or incomplete node inventory: record exact identities, observed state/time, denied action and unmet condition; pause. Do not relax the gate, relabel simulated evidence live or treat a missing series as zero. Retain request history, selected skill commit and operation IDs through the pause.

On completed replacement verify both original nodes absent, desired capacity restored to two, retained nodes healthy/protected as recorded, app traffic evidence complete and no unknown operation remains. Record actual failed/ambiguous operations and costs separately. Successful node removal alone does not establish successful test acceptance.
