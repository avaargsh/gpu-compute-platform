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
- Reconciliation is at-least-once: provider side effects use deterministic identity and same-generation adoption rather than claiming exactly-once remote execution.
- Reconcile side effects require a remote lease; the lease limits concurrent reconcilers but does not make an in-flight provider call atomic with lease expiry.
- Observation and finalization writes are fenced by the current unexpired reconcile-lease owner; an old Agent cannot commit after ownership moves.
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

Check the production image policy (digest pinning + cosign once production manifests exist):

```bash
make supply-chain-check
```

Run the real acceptance path:

```bash
make e2e-golden
```

For HA/debugging, a cluster-agent process may use an explicit lease identity and
sync interval:

```bash
AGENT_INSTANCE_ID=agent-a AGENT_SYNC_INTERVAL=1s \
  CLUSTER_ID=kind-golden CONTROL_PLANE_URL=http://127.0.0.1:8080 \
  ./cluster-agent
```

If `AGENT_INSTANCE_ID` is omitted, the Runner generates a random process lease
owner. These settings do not introduce cluster-wide leader election; leases
remain resource-scoped.

The Golden Path creates a kind cluster, installs Kueue and Fake GPU Operator, configures a stable H100 profile, and validates Control Plane -> Agent -> Kueue -> Job/Pod -> Observation -> Finalizer/Delete. It also runs two real cluster-agent processes to prove lease fencing before expiry, takeover after process death/expiry, stable Job UID across takeover, generation fencing, evidence completeness, and immutable workload replacement.

See [docs/CONTROL_PLANE_V2_GO.md](docs/CONTROL_PLANE_V2_GO.md) for the architecture contract, [docs/WORKLOAD_LIFECYCLE.md](docs/WORKLOAD_LIFECYCLE.md) for the Reserve → Allocate → Bind → Release → Audit workload lifecycle, [docs/PROVIDER_RECOVERY_CONTRACT.md](docs/PROVIDER_RECOVERY_CONTRACT.md) for the at-least-once provider recovery model, [docs/RELEASE_ACCEPTANCE_V0_1.md](docs/RELEASE_ACCEPTANCE_V0_1.md) for the v0.1 release gate, and [docs/CAPABILITY_RELEASE_MATRIX.md](docs/CAPABILITY_RELEASE_MATRIX.md) for the supported/deferred capability matrix and promotion rules.
