# Provider Architecture

Status: **Stage B provider execution boundary**.

The current platform does not implement a cloud-GPU broker and does not choose between Alibaba Cloud, Tencent Cloud, RunPod, KAI, Volcano, HAMi, or DRA at runtime. The supported execution path is intentionally narrower:

```text
ClusterBinding
  provider = kueue
        |
        v
Desired ComputePool / Workload
  frozen provider identity
        |
        v
Cluster Agent
        |
        v
provider.Adapter registry
        |
        v
Kueue adapter
        |
        v
Kubernetes / Kueue / accelerator resources
```

## Provider SPI

The execution boundary lives in `internal/provider`:

```go
type Adapter interface {
    PoolProvider
    WorkloadProvider
}
```

A provider implements four lifecycle operations:

```go
ReconcilePool(context.Context, PoolProjection) (PoolObservation, error)
DeletePool(context.Context, PoolProjection) (DeletionObservation, error)
ReconcileWorkload(context.Context, WorkloadProjection) (WorkloadObservation, error)
DeleteWorkload(context.Context, WorkloadProjection) (DeletionObservation, error)
```

The SPI is deliberately small. It does not own:

- placement policy;
- scheduler selection;
- a second desired-state model;
- generic Kubernetes CRUD;
- provider-specific billing APIs;
- workflow orchestration.

## Provider identity invariant

Provider identity is frozen before execution.

The management plane projects the provider from the relevant binding into the desired ComputePool or Workload. The Cluster Agent dispatches only that identity through its local adapter registry.

Missing or unknown provider names fail closed. Capability discovery never guesses or silently substitutes a provider.

Today the production Cluster Agent registers only:

```text
kueue -> internal/provider/kueue
```

## Portable accelerator binding

Product intent uses portable accelerator classes. Provider-specific details are resolved through bindings instead of leaking into the public product model.

Example:

```text
portable class: h100-80g
        |
        v
AcceleratorBinding
  resourceName
  flavor
  node selectors / provider facts
        |
        v
Kueue / Kubernetes projection
```

This keeps the control plane independent from a single vendor resource name while still making the concrete execution projection deterministic.

## Recovery contract

Every provider adapter must obey the same recovery model described in `PROVIDER_RECOVERY_CONTRACT.md`:

- deterministic provider-side identity;
- desired generation markers;
- create-or-adopt instead of blind recreate;
- same-generation adoption after lost ACK;
- rejection of conflicting generation ownership;
- resource-scoped reconcile lease;
- lease-owned reporting and finalization;
- observe-until-gone deletion;
- durable tombstone/finalization semantics.

The lease limits concurrent reconcilers. It does not make a remote Kubernetes/API side effect atomic with lease expiry.

## Capability discovery

Cluster capability discovery records facts such as:

- Kubernetes server version;
- Kueue availability;
- discovered scheduler names;
- DRA API availability/version;
- accelerator classes.

These are **facts**, not scheduling policy.

The platform may use those facts later for placement preflight or compatibility checks, but discovery does not register a new provider and does not change the frozen provider identity of an existing resource.

## Adding a second provider

A second adapter must not be registered merely because the corresponding scheduler or API is present.

Before promotion it must independently prove:

1. deterministic pool/workload identity;
2. generation-safe create/adopt/delete;
3. lost-ACK recovery;
4. crash/takeover behavior under lease transfer;
5. capability facts needed by the adapter;
6. deletion/finalization recovery;
7. a provider-specific acceptance path that does not weaken the existing Kueue Golden Path.

Until those gates are satisfied, DRA/HAMi/KAI/Volcano remain capability/research signals rather than supported execution providers.

## Related documents

- `PROVIDER_ADAPTER_SPI.md` — Stage B SPI and registration rules
- `PROVIDER_RECOVERY_CONTRACT.md` — side-effect and recovery semantics
- `CONTROL_PLANE_V2_GO.md` — management/control-plane boundaries
- `CAPABILITY_RELEASE_MATRIX.md` — supported/deferred capabilities
- `RELEASE_ACCEPTANCE_V0_1.md` — release evidence and gates
