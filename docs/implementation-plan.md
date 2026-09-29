# AI Compute Control Plane v2 — Phase 0 Implementation Plan

## Scope

Phase 0 proves one thing: portable desired state can converge through Kueue/Kubernetes
and runtime reality can be projected back into the platform state model.

Out of scope for the Golden Path: DRA, HAMi, Volcano, KAI, Serving, MultiKueue,
cloud node provisioning, and NPU backends.

## Authority model

- PostgreSQL is the source of truth for platform desired state and the persisted
  observed projection.
- Kubernetes/Kueue is execution reality.
- Observed state is produced only by controller/provider observation; public APIs
  never mutate it directly.
- Desired mutations increment generation.
- Observed writes are accepted only when generation still matches.
- Side effects are serialized with the database reconcile lease.

## Stable abstractions

- **AcceleratorClass = WHAT**: portable accelerator requirement.
- **ComputePool = WHERE + POLICY**: queue, quota, scheduling policy, placement domain.
- **AcceleratorBinding = HOW**: provider-private quota/device/placement realization.

Provider resource names, node names, physical GPU IDs, K8s UIDs, Celery task IDs,
cloud instance IDs, and vendor-specific resources must not enter portable APIs.

## Responsibility split

- **Kueue**: admission and quota.
- **Scheduler / Volcano / KAI**: placement and gang semantics.
- **Extended resources / HAMi / DRA**: device allocation and isolation.
- **ComputeProvider**: materialize and observe provider resources without redefining
  admission semantics.

The stable provider SPI is:

- ensure_pool
- delete_pool
- resolve_binding
- materialize_workload
- observe_workload
- report_capacity

## Core status model

Portable conditions describe lifecycle facts. Device-specific details belong in
`status.allocation` or `status.provider_status`.

Minimum lifecycle facts for Phase 0:

- Ready
- Admitted
- Progressing
- Degraded

The platform may expose a convenience phase, but conditions remain the composable
facts. Every controller-produced condition carries `observedGeneration`.

`ObservedState` additionally records:

- observed_generation
- reconcile_revision
- evidence_refs
- provider_ref
- optional allocation observation

## Golden Path

```text
Project
  -> ComputePool desired
  -> ComputePool reconciler
  -> ResourceFlavor / ClusterQueue / LocalQueue
  -> Pool Ready
  -> Workload desired
  -> Kubernetes Job
  -> Kueue Workload
  -> Admitted
  -> Pods Ready
  -> ObservedState
  -> terminal result
```

A CPU/fake-accelerator path is sufficient for Phase 0 E2E. GPU integration is not
a Phase 0 acceptance requirement.

## Epics

### E1 — Contract

- Freeze Project / AcceleratorClass / ComputePool / Workload contracts.
- Keep provider-private mappings inside ComputePool bindings.
- Schema changes only through Alembic.
- Maintain an OpenAPI contract snapshot.

### E2 — Reconcile

- generation / observed_generation guard.
- DB lease around side effects.
- reconcile_revision attached to controller observations/evidence.
- fail-closed Kubernetes access.
- periodic sweep plus Celery wake-up; Celery is not state.

### E3 — Kueue

- ComputePool -> ResourceFlavor / ClusterQueue / LocalQueue.
- Workload -> Job with queue binding.
- Observe Kueue Workload admission status.
- Delegate pods-ready timeout/requeue semantics to Kueue where possible.

### E4 — Golden Path

A single automated scenario must prove:

```text
Project -> Pool -> Workload -> Admitted -> Pods Ready -> Observed
```

## Phase 0 Definition of Done

- Golden Path automated test passes consistently.
- Pool-not-ready blocks Workload materialization with a readable condition.
- No legacy GPU task/DAG/provider imports remain in the v2 path.
- Alembic can upgrade and downgrade one revision.
- Public API does not expose provider-private identifiers.
- Documentation and .env.example match runtime behavior.

Suggested operator flow:

```text
make kind-up
make install-kueue
make migrate
make run
make e2e-golden
```

Only after this is green should Phase 1 add HAMi production bindings and isolated
DRA pilot pools.
