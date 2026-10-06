# Stage B Provider Adapter SPI

Status: **Stage B execution boundary**.

The control plane does not choose a scheduler inside the Cluster Agent. A
`ClusterBinding` already binds a ComputePool to one cluster and one immutable
provider identity. The Stage B SPI carries that frozen identity to the provider
execution boundary.

```text
ClusterBinding
  clusterId
  provider = kueue
        |
        v
desired ComputePool / Workload
  provider = kueue
        |
        v
Cluster Agent
        |
        v
provider.Adapter registry
        |
        +--> kueue (registered today)
        |
        +--> future provider (not registered until independently accepted)
```

## Contract

A provider adapter implements the existing pool and workload lifecycle:

```go
type Adapter interface {
    PoolProvider
    WorkloadProvider
}
```

The SPI does not add a second lifecycle, scheduler, or placement policy.
Reconcile/delete semantics remain those in
[PROVIDER_RECOVERY_CONTRACT.md](PROVIDER_RECOVERY_CONTRACT.md):

- deterministic provider identity;
- desired-generation markers;
- create-or-adopt;
- lost-ACK recovery;
- lease-owned reporting/finalization;
- observe-until-gone deletion.

## Identity invariant

The management plane writes the provider from the frozen `ClusterBinding` into
the desired ComputePool and Workload specs. The Cluster Agent dispatches only
that provider name.

Missing or unknown provider identity fails closed. The Runtime never guesses a
provider from discovered cluster capabilities.

The Cluster Agent reports its sorted runtime registry as
`ClusterCapabilities.providers`. This is the only capability fact that means
"this binary can dispatch this provider identity"; scheduler, DRA, and CRD
discovery remain separate environment facts.

Today only:

```text
kueue -> internal/provider/kueue
```

is registered by the production Cluster Agent.

## Adding a second provider

A second provider is not admitted merely because a scheduler/API is discovered.
Before registration it must prove:

1. the same provider recovery contract;
2. deterministic pool/workload execution identities;
3. generation-safe create/adopt/delete;
4. crash/takeover and lost-ACK behavior;
5. capability facts required by that adapter;
6. a provider-specific acceptance path without changing the frozen v0.1 Kueue
   Golden Path.

Provider selection policy remains a separate future concern. Stage B only makes
the execution boundary explicit.
