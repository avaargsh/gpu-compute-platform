# Finalizer / Deletion baseline

Frozen software baseline on 2026-09-30.

- Branch: `feat/control-plane-v2-go`.
- Verified code revision: `b3d3b0a119bb2c099c3803f0f137a595cda14c01`.
- Exact-revision [push workflow](https://github.com/avaargsh/gpu-compute-platform/actions/runs/36686628367).
- Preceding frozen [P0.5 durability baseline](p0.5-reconcile-durability.md):
  `1f45cc15ca1e7571441dbe72dd160d0c067310f2`.
- Accelerator P1 remains frozen at
  `00ca91eeb39a17c4556bcd17ac2b5781604f4b78`.

## Acceptance gates

The exact code revision passes tidy-module checks, format, vet, all Go tests
including PostgreSQL 17 contracts, both binary builds, both container targets,
and the kind + Kueue Golden Path. Only push CI is used to accept this baseline.

Regression coverage verifies missing-desired Report/publication ordering, a
PostgreSQL report blocked on an absent resource identity, reversed observation
batches, evidence prerequisites, preservation of unknown finalizers, atomic
rollback on hard-delete failure, tombstone retention, retry/backoff while
provider resources remain, and cleanup of retries after finalization/disappearance.

Golden Path deletes an unrealizable workload, a realized Job, and its pool.
Provider resources are absent before desired hard deletion; shared
ResourceFlavor remains. The internal state route exposes `Finalized` with
current-generation final conditions, nonempty evidence references, valid
condition transition time and finalization time. Desired/observed runtime
generations are zero after cleanup.

## Frozen lifecycle

1. Mark desired with an idempotent deletion timestamp and the owned cleanup finalizer.
2. Acquire the current resource lease; clean provider resources and observe gone.
3. Durably report final generation/evidence under the current lease.
4. Atomically preserve the tombstone, remove only the owned finalizer, hard delete
   desired, and clear runtime observation/lease.
5. Clear local retry state after successful finalize or subsequent desired absence.

Newer desired generations may recreate the identity while retaining its last
deletion evidence. Older generations remain fenced. Report before desired exists
is supported; creation atomically discards only incompatible provisional
observations. Existing generation drift stays observable.

See [the design contract](../finalizer-deletion.md) for PostgreSQL lock ordering
and evidence requirements. This is a CPU-only simulated accelerator software
baseline, not a real GPU performance or production-readiness claim.

## Change boundary

PR #2 remains closed and unmerged; `master` is untouched. Placement Migration
stays retired. DRA, HAMi, MIG, multi-provider work and a full Python-to-Go rewrite
remain outside scope. Replacing this baseline requires a new exact-HEAD green
push workflow, including Golden Path.
