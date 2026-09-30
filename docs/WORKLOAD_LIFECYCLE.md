# Workload Accelerator Lifecycle

The v2 control plane treats accelerator allocation as a provider lifecycle, not as a second scheduler.

```text
Portable Workload Intent
        |
        v
Reserve
  Kueue QuotaReserved
        |
        v
Allocate
  extended-resource request
  OR DRA ResourceClaim
        |
        v
Bind
  Kueue Admitted -> Pod scheduled/ready
        |
        v
Run
  provider observation + evidence
        |
        v
Release
  delete Job -> delete workload-owned ResourceClaim
        |
        v
Audit
  final observation -> evidence refs -> finalizer/tombstone
```

## Ownership boundary

The control plane owns portable intent, desired generation, lifecycle/finalization state and durable evidence references.

The cluster agent owns reconciliation side effects.

Kueue owns quota reservation/admission. Kubernetes and the accelerator provider own concrete device allocation and pod binding. The platform must not reproduce scheduler or DRA internals.

## Allocation modes

### Extended resource

A portable accelerator class resolves to a concrete resource name and Kueue flavor.

```text
h100-80g
  -> allocationMode: extended-resource
  -> nvidia.com/gpu
  -> ResourceFlavor h100-80g
```

MIG remains a partition of this model when exposed as an extended resource.

### DRA

A portable accelerator class resolves to a DRA DeviceClass and the workload owns a ResourceClaim.

```text
portable class
  -> allocationMode: dra
  -> ResourceClaim
  -> Pod resourceClaim reference
```

The ResourceClaim is workload-owned lifecycle state. It must be removed only after the Job has been removed or observed gone.

## Lifecycle evidence contract

The provider returns stable evidence references for resources that participated in allocation and release.

For an extended-resource workload:

```text
k8s://<cluster>/namespaces/<ns>/jobs/<job>
kueue://<cluster>/namespaces/<ns>/workloads/<workload>
```

For a DRA workload:

```text
k8s://<cluster>/namespaces/<ns>/jobs/<job>
k8s://<cluster>/namespaces/<ns>/resourceclaims/<claim>
kueue://<cluster>/namespaces/<ns>/workloads/<workload>
```

Release evidence is not proof that a resource still exists. It is an audit pointer identifying the provider object whose deletion was reconciled.

## Release invariants

1. Deletion is idempotent and replay-safe.
2. Workload cleanup is independent of ComputePool readiness; a failed/stale pool must not strand workload-owned resources.
3. Job deletion is reconciled before DRA ResourceClaim deletion.
4. A DRA workload is not `Gone` until both Job and ResourceClaim are gone.
5. Final deletion evidence is reported before desired state is finalized.
6. Failed evidence reporting or finalization replays provider deletion safely.
7. ComputePool cleanup waits until dependent Workloads have finalized.
8. Shared pool resources such as ResourceFlavor are not deleted by a workload release.
9. Provider-created Job and ResourceClaim objects carry the desired generation. An existing object from another generation fails closed instead of being reported as converged.
10. `ObservedGeneration` records the desired generation the agent processed, including failed reconciliation; convergence is expressed by conditions. A stale provider object therefore reports `Ready=False / ReconcileFailed` for the current generation rather than success.
11. v0.1 does not hot-replace immutable Jobs. The public Workload API treats an active Workload as immutable execution intent: identical same-generation PUTs are idempotent, while same-generation spec mutations and higher-generation in-place replacements return conflict.
12. A changed Workload follows Delete -> provider cleanup -> Finalize -> Recreate with a generation higher than the tombstone. Internal desired-state/migration APIs remain lower-level mechanisms and are not the public replacement contract.

These invariants define the v0.1 workload lifecycle boundary. Future DRA/HAMi/provider integrations must fit this contract rather than add a parallel lifecycle model.
