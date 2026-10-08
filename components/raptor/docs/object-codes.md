# Catalog object codes

Applications use `A00001`, databases `D00001`, and database proxies `DP00001`.
The code is independent of environment and relationships. Internal UUID object
IDs remain unchanged. PostgreSQL owns separate atomic sequences for each kind.
Five digits is minimum padding: `A99999` is followed by `A100000`. Failed inserts
may consume sequence values; gaps are intentional. Deleted codes are not reused.
The create API generates codes and the edit API cannot change them.

## Existing objects

Owner migration `004_object_codes.sql` converts existing nonconforming catalog
codes once, ordered by kind and internal UUID. Already formatted codes are
reserved and kept. It records old code, unchanged object ID, kind, and new code
in `raptor.object_code_aliases`. Replaying migrations neither renumbers objects
nor resets sequences. Legacy lookups resolve to the current object within its
kind. New agent and direct requests resolve aliases to canonical codes before
persisting their definitions; retry hashes still use the original caller input.

Saved request definitions, hashes, approval bindings, events, results, and
Gateway records are immutable historical evidence and are not rewritten.
Aliases support catalog lookup; they do not make old cloud labels discoverable
under the new code. Existing terminal requests remain readable. The continue action rejects legacy-code bindings; create a new request with the
canonical codes instead. Historical requests must not be replayed as a shortcut
to migration.

## Coordinated live migration

1. Verify the actual deployed revision and read every catalog object and its
   discovered deployment. Record internal UUID, kind, name, old code, current
   deployment IDs/UIDs and environment. Back up the catalog privately.
2. Stop new submissions and dispatch using the existing maintenance/release
   mechanism. Wait for existing requests and attempts to finish and for
   credential cleanup. The first migration refuses any nonterminal Raptor
   request. Do not force its status to terminal in SQL.
3. Build/test the Raptor revision and run the existing owner migration entry
   point (`raptor-backend --migrate`, private `MIGRATION_DATABASE_URL`). Run
   against a local restored backup first. The database migration is one
   transaction under the migration advisory lock. No secret values belong in
   commands, logs, source, or Terraform state.
4. Export the actual committed mapping through a private database session:

   ```sql
   SELECT o.id, o.kind, o.name, a.code AS old_code, o.code AS new_code
   FROM raptor.objects o
   LEFT JOIN raptor.object_code_aliases a ON a.object_id=o.id
   ORDER BY o.kind,o.id;
   ```

5. Commit the same mapping to GitOps configuration: platform Helm `appCodes`,
   test-client application codes, database/proxy Terraform inputs/tags and
   proxy target database references. Update other code-bound metadata,
   secret/config identity fields and metric labels only where they actually
   exist. Preserve credential values and resource identity. Versioned private
   proxy configuration may require a new configuration version.
6. Terraform owns Alibaba Cloud tags and module inputs. Inspect a plan and
   apply only expected in-place changes; never replace a resource to change
   its catalog code. GitOps owns Kubernetes labels. Update workload metadata
   and pod-template labels consistently; retain immutable selectors and
   Service routing. A pod-template label update can cause a rollout, which
   must be accounted for in the maintenance window.
7. Release the tested generator. Verify every object has the expected prefix,
   resolves by unchanged UUID, and discovers the exact same cloud resources
   with the new labels/tags. Verify proxy-to-database relationships and UI
   details. Then reopen submissions and verify the next generated code for
   each kind. Retain the mapping, revision, Terraform plan and verification
   receipt.

Catalog migration alone is not completion of the live resource migration.
Stop and reconcile if any provider reference has no matching catalog object,
any resource replacement appears in the plan, or the inventory is incomplete.
