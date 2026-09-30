# Finalizer / Deletion contract

Scope: `feat/control-plane-v2-go`, following the frozen P0.5 reconcile durability
baseline. Accelerator P1 remains frozen at
`00ca91eeb39a17c4556bcd17ac2b5781604f4b78`.

Deletion marks desired state with an idempotent UTC `deletionTimestamp` and
`gpu-compute-platform.io/provider-cleanup` finalizer. Desired state remains
visible until the provider cleanup has observed its resources gone. Pool
cleanup waits for dependent workloads; shared ResourceFlavors remain intact.

The agent reports current-generation `Ready=False`, `reason=Deleted` and
nonempty provider evidence references under its active reconcile lease. Only
then may finalization preserve that evidence in a tombstone, remove the owned
finalizer, and hard delete desired state. Unknown finalizers block hard deletion.
PostgreSQL commits the tombstone, finalizer removal, desired deletion,
observation deletion and lease deletion in one transaction. A failed commit
leaves all of them replayable. Memory applies the same contract under its lock.
Local retries clear after successful finalization or a subsequent desired pull
that no longer includes the resource.

The tombstone retains the finalized generation, final conditions, evidence
references and finalization time. Reports or desired publications at or below
that generation are fenced. A higher generation may recreate the identity
without erasing its last deletion evidence. The internal resource state route
returns `Finalized` plus `deletionTombstone` when desired is absent; runtime
desired/observed generations are zero and public resource GET remains absent.
After recreation, the internal route projects the current desired/observation.
Tombstones retain the last deletion per identity; this is not an unbounded
event archive or a new provider surface.

## Missing-desired observation CAS

Reports before desired publication remain supported. PostgreSQL lifecycle
writers lock a stable resource identity with a transaction advisory lock before
row locks, using READ COMMITTED snapshots. Report locks an entire batch in
sorted lock-key order. This covers absent rows and reversed batches. Lease
expiration checks use wall time after lock waits.

If Report commits first, desired creation keeps only a provisional observation
whose generation matches the published generation. If desired creation commits
first, Report checks its current generation before writing. Existing desired
updates retain prior observations so generation drift remains observable.
Memory uses its existing global mutex for the same ordering.

## Verification

Regression coverage exercises evidence prerequisites, unknown finalizers,
tombstone retention, missing-desired publication order, a blocked PostgreSQL
report, reversed report batches, transaction rollback on final hard-delete
failure, provider absence/backoff, and local retry cleanup. The kind+Kueue
Golden Path checks provider absence before finalization and final evidence after
runtime state disappears. Freeze only after the exact HEAD push CI passes
format, vet, tests, PostgreSQL contracts, builds, container targets and Golden
Path. Placement Migration stays retired; DRA/HAMi/MIG/multi-provider work is
outside this change.
