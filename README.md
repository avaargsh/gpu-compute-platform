# AI Compute Control Plane

Kubernetes-native **Go** control plane for portable accelerator workloads.

> **Status:** alpha. The legacy Python control plane has been retired on this branch. The Go control plane and cluster agent are the canonical implementation.

## Runtime boundary

```text
Client / CLI
    |
    | REST /api/v1
    v
Go Control Plane
    |
    | desired state / generation / finalizers
    v
PostgreSQL
    |
    | pull / report / reconcile lease
    v
Cluster Agent
    |
    | provider reconciliation
    v
Kubernetes + Kueue + accelerator provider
    |
    | observed state / evidence
    v
Go Control Plane
```

## Current Golden Path

```text
Project
  -> ComputePool
  -> Accelerator Binding
  -> Workload
  -> Kueue admission
  -> Job / Pod
  -> Observation / Evidence
  -> Finalizer / Provider Cleanup
  -> Tombstone / Hard Delete
```

The current CPU-only acceptance environment uses Run:ai Fake GPU Operator with an H100 profile. The operator owns simulated hardware facts; the platform consumes them through the portable `h100-80g` binding.

## Invariants

- PostgreSQL is the management-plane source of truth.
- Kubernetes is an execution provider, not a second product source of truth.
- ComputePool accelerator classes are portable intent; provider bindings map them to resource names, flavors, and node labels.
- The cluster agent owns provider reconciliation and downstream observation.
- Desired and observed state are generation-aware.
- Reconcile side effects require a remote lease.
- Deletion retains Desired state until provider cleanup is observed complete.
- Finalization writes a generation tombstone and removes observation/lease state atomically in PostgreSQL.
- Shared cluster-scoped resources such as Kueue ResourceFlavor are not garbage-collected by a single ComputePool without explicit ownership/reference tracking.
- DRA, HAMi, MIG, multi-provider expansion, and advanced placement are intentionally deferred until the current Golden Path is frozen.

## Development

Requires Go 1.24+, Docker, kind, kubectl, and Helm for the full Golden Path.

```bash
make fmt-check
make vet
make test
make build
```

Run the deterministic release contracts:

```bash
make acceptance-contract
```

Run the real acceptance path:

```bash
make e2e-golden
```

The Golden Path creates a kind cluster, installs Kueue and Fake GPU Operator, configures a stable H100 profile, and validates Control Plane -> Agent -> Kueue -> Job/Pod -> Observation -> Finalizer/Delete, including agent restart replay, generation fencing, evidence completeness, and immutable workload replacement.

See [docs/CONTROL_PLANE_V2_GO.md](docs/CONTROL_PLANE_V2_GO.md) for the architecture contract, [docs/WORKLOAD_LIFECYCLE.md](docs/WORKLOAD_LIFECYCLE.md) for the Reserve → Allocate → Bind → Release → Audit workload lifecycle, and [docs/RELEASE_ACCEPTANCE_V0_1.md](docs/RELEASE_ACCEPTANCE_V0_1.md) for the v0.1 release gate.
