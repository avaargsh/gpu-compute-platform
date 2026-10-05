# Stage B Provider Conformance

Status: **implementation contract before centralized validation**.

Stage B allows the project to implement one additional scheduler-backed
execution adapter without immediately promoting it into the supported surface.

The purpose is to keep future provider work aligned with the existing Kueue
lifecycle instead of letting every scheduler introduce its own control plane.

## Three different facts

Do not collapse these into one concept:

| Fact | Owner | Meaning |
| --- | --- | --- |
| `schedulers[]` | cluster discovery | scheduler software observed in the Kubernetes cluster |
| `providerAdapters[]` | Cluster Agent binary | provider adapters registered in the running Agent |
| `ClusterBinding.provider` | management plane | immutable adapter selected for a ComputePool |

Discovery does not select a provider. An adapter being compiled does not prove
the matching scheduler is installed. A binding does not authorize the Runtime
to guess or substitute another adapter.

## Current state

```text
product provider catalog
└── kueue

Cluster Agent registry
└── kueue

accepted execution path
└── kueue
```

KAI, Volcano, DRA, HAMi and serving integrations are not registered providers.

## Functional conformance contract

Before a candidate adapter is registered, it must implement the existing
`provider.Adapter` boundary:

```go
type Adapter interface {
    PoolProvider
    WorkloadProvider
}
```

That means the candidate must define mappings for all four operations:

```text
ReconcilePool
DeletePool
ReconcileWorkload
DeleteWorkload
```

and preserve the same management-plane semantics.

### Pool projection

The adapter receives an already-bound provider identity plus:

- PoolID / ProjectID / ClusterID
- Namespace
- desired Generation
- portable accelerator requests
- accelerator bindings
- scheduling intent

It may translate those fields to scheduler-native resources, but must not write
scheduler-specific fields back into the public portable API.

### Workload projection

The adapter receives:

- immutable provider identity
- WorkloadID / PoolID / ProjectID / ClusterID
- Namespace
- desired Generation
- image / command
- portable accelerator request
- resolved accelerator binding

The management plane does not pass a node name, GPU UUID or scheduler-selected
device identity as desired state.

## Recovery conformance

Every candidate must reuse
[PROVIDER_RECOVERY_CONTRACT.md](PROVIDER_RECOVERY_CONTRACT.md):

1. deterministic provider object identity;
2. desired-generation marker;
3. create-or-adopt on same generation;
4. fail closed on conflicting generation;
5. lost-ACK replay without a second logical side effect;
6. independent observation after mutation;
7. lease-fenced observation/finalization;
8. observe-until-gone deletion;
9. deletion tombstone semantics owned by the existing control plane.

A candidate that requires a parallel desired-state database, a separate lease
system or its own deletion state machine does not conform to this SPI.

## Error boundary

Provider-specific API failures must be normalized into the existing provider
error model. The Runner decides retry vs terminal observation; adapters must not
invent another retry controller.

## Capability boundary

A future candidate may extend discovery with factual metadata required for
compatibility checks. Those fields must remain observations such as:

- scheduler name/version;
- API/CRD availability/version;
- accelerator classes;
- required Kubernetes feature availability.

Do not expose scheduler scores, queue internals, node-ranking state or
provider-specific placement algorithms as control-plane policy.

## Implementation sequence

For the next candidate:

```text
1. provider package + projection mapping
2. deterministic identity mapping
3. pool/workload reconcile + delete
4. observation/evidence mapping
5. discovery facts if required
6. keep adapter OUT of production registry
7. centralized validation later
8. only then add to provider catalog + Agent registry
```

This lets feature implementation proceed now while preserving a hard promotion
boundary for later testing.

## Promotion gate

Registration is a separate change from implementation.

Only the centralized validation phase may change:

```text
provider.IsSupportedAdapter(candidate) = true

Runtime registry:
  candidate -> adapter
```

That promotion must include the executable recovery evidence and real-GPU
acceptance required by
[CAPABILITY_RELEASE_MATRIX.md](CAPABILITY_RELEASE_MATRIX.md).
