# AI Compute Control Plane v2 — Go Architecture

## Implemented product domain

```text
Project
├── ProjectBinding
├── ComputePool
│   └── ClusterBinding
└── Workload
```

Serving/model objects remain future architecture and are not part of the current
Go product contract.

## Runtime boundary

```text
Client / CLI
    |
    v
Go Control Plane
    |
    | PostgreSQL desired state
    v
Cluster Agent
    |
    | desired provider identity
    v
provider.Adapter registry
    |
    +--> kueue
    v
Kubernetes / Kueue / accelerator stack
    |
    v
Observation + Evidence
    |
    v
PostgreSQL
```

## Source of truth

PostgreSQL is the management-plane source of truth.

Kubernetes resources are projections of platform desired state. They are not a
second product database.

## Core invariants

1. ComputePool is the portable capacity contract.
2. Workload references a ComputePool and does not persist scheduler-selected
   node/GPU identities.
3. Project placement and pool placement are explicit bindings.
4. `ClusterBinding.clusterId` and `provider` are immutable while active.
5. Accelerator classes are portable; concrete resource names/flavors/node labels
   live in bindings.
6. Desired and observed state are generation-aware.
7. The Cluster Agent owns provider mutation and independent observation.
8. Resource-scoped leases fence observation/finalization, not the remote API call.
9. Reconciliation is at-least-once with deterministic provider identity.
10. Capability registration reports facts; it never selects a scheduler/provider.
11. Unsupported provider adapter names fail before entering a new binding.
12. The public API does not expose generic Kubernetes CRUD as the product model.

## Provider identity path

```text
PUT ClusterBinding(provider=kueue)
        |
        | product catalog validation
        v
immutable binding
        |
        v
desired ComputePool / Workload provider=kueue
        |
        v
Cluster Agent Runtime registry
        |
        v
internal/provider/kueue
```

The Runtime must not derive a provider from scheduler discovery.

## Cluster capability registration

The Agent publishes two classes of facts:

### Cluster-observed facts

- Kubernetes version;
- scheduler names and observable versions;
- DRA API availability/version;
- portable accelerator classes.

### Agent execution facts

- `providerAdapters[]`: adapter names registered in the running Agent.

Example:

```json
{
  "schedulers": [{"name": "kueue", "version": "v0.19.6"}],
  "draApiAvailable": true,
  "draApiVersion": "resource.k8s.io/v1",
  "accelerators": ["h100-80g"],
  "providerAdapters": ["kueue"]
}
```

A cluster can theoretically report an observed scheduler without having a
promoted execution adapter for it. That distinction is required for safe Stage B
experimentation.

## Repository runtime shape

```text
cmd/
  control-plane/
  cluster-agent/

internal/
  agent/
  cluster/
  domain/
  platform/httpapi/
  provider/
    kueue/
  store/
    agentstore/
    postgres/
```

The active system remains a modular monolith plus Cluster Agent. Service
decomposition is deferred.

Historical Python application/frontend code is not part of the canonical runtime
path.

## Stage B boundary

Current:

```text
Kueue provider: supported
Provider SPI: implemented
Provider catalog: kueue only
Second provider: not registered
DRA execution: internal ResourceClaim primitive exists; public path disabled
HAMi/fractional: not implemented
Serving: not implemented
Multi-cluster placement policy: not implemented
```

A second provider can be implemented behind the SPI before validation, but
promotion is a separate catalog + Runtime registration change.

See:

- [PROVIDER_ADAPTER_SPI.md](PROVIDER_ADAPTER_SPI.md)
- [PROVIDER_RECOVERY_CONTRACT.md](PROVIDER_RECOVERY_CONTRACT.md)
- [STAGE_B_PROVIDER_CONFORMANCE.md](STAGE_B_PROVIDER_CONFORMANCE.md)
