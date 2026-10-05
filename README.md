# AI Compute Control Plane

Kubernetes-native **Go** control plane for portable accelerator workloads.

> **Status:** alpha, Stage B. The Go control plane and Cluster Agent are the
> canonical implementation. The historical Python application, frontend, and
> cloud-provider code remain only as legacy reference and are not part of the
> default build, Compose stack, CI, or current product contract.

## Current product boundary

```text
Client / CLI
    |
    | REST /api/v1
    v
Go Control Plane
    |
    | PostgreSQL desired state
    | ProjectBinding / ClusterBinding
    v
Cluster Agent
    |
    | frozen provider identity
    v
Provider Adapter Registry
    |
    +--> kueue   [supported]
    |
    +--> future adapters [not registered]
    v
Kubernetes + scheduler / accelerator stack
    |
    v
Observation + Evidence
```

The platform owns desired state, lifecycle, provider binding, observation,
evidence, lease fencing and finalization. It does **not** implement a second
scheduler or device allocator.

## Supported execution path

The only supported provider adapter today is:

```text
ClusterBinding.provider = kueue
  -> internal/provider/kueue
  -> Kueue admission
  -> Kubernetes Job / Pod
```

A binding to an adapter that is not in the product provider catalog is rejected
at the API boundary. The Cluster Agent also publishes its registered adapter
names in `ClusterCapabilities.providerAdapters`.

Scheduler discovery is separate:

- `schedulers[]` = scheduler facts observed in the cluster;
- `providerAdapters[]` = execution adapters registered in the Agent binary;
- `ClusterBinding.provider` = the immutable adapter selected for one pool.

The Runtime never chooses a provider from discovered capabilities.

## Current Golden Path

```text
Project
  -> ProjectBinding
  -> ComputePool
  -> ClusterBinding(provider=kueue)
  -> Accelerator Binding
  -> Workload
  -> Kueue admission
  -> Job / Pod
  -> Observation / Evidence
  -> Finalizer / Provider Cleanup
  -> Tombstone / Hard Delete
```

The acceptance environment uses Run:ai Fake GPU Operator with an H100 profile.
That validates the portable accelerator contract and Kueue lifecycle; it is not
a claim of real-GPU certification.

## Stage B status

Completed:

- capability inventory: scheduler name/version, DRA API availability/version,
  portable accelerator classes;
- immutable cluster + provider binding identity;
- provider identity copied into desired ComputePool / Workload state;
- explicit provider adapter SPI;
- fail-closed dispatch for missing/unknown adapter;
- Kueue preserved as the only production adapter;
- existing Kueue recovery and Golden Path remain the compatibility baseline.

Next functional slice:

1. choose **one** candidate scheduler adapter (KAI or Volcano);
2. write its projection/identity/recovery mapping against the existing SPI;
3. keep it unregistered until the later centralized acceptance pass;
4. only after contract + real-GPU evidence promote it into the capability matrix.

DRA execution, HAMi/fractional GPU, serving providers and multi-cluster placement
remain later slices.

## Invariants

- PostgreSQL is the management-plane source of truth.
- Kubernetes resources are provider projections, not a second product source of truth.
- Accelerator classes are portable intent; provider bindings contain resource names,
  flavors and node labels.
- `ClusterBinding.clusterId` and `ClusterBinding.provider` are immutable while active.
- Reconciliation is at-least-once, with deterministic provider identity and
  same-generation adoption.
- Observation/finalization are fenced by the current resource lease owner.
- Deletion observes provider cleanup before finalization and tombstone commit.
- Capability registration reports facts; it does not perform placement or provider selection.
- Unsupported provider identities fail before entering active bindings.

## Run locally

Requirements: Go 1.24+, Docker, and PostgreSQL. kind/kubectl/Helm are needed for
the full Kueue Golden Path.

```bash
make fmt-check
make vet
make test
make build
```

Default container build now produces the Go control plane:

```bash
docker build -t gpu-compute-control-plane .
docker compose up --build
```

To also start a Cluster Agent against a kubeconfig:

```bash
cp .env.example .env
# edit KUBECONFIG and CLUSTER_ID
docker compose --profile agent up --build
```

Development Compose also uses the Go binaries:

```bash
docker compose -f docker-compose.dev.yml up --build
```

## Acceptance

Deterministic lifecycle contracts:

```bash
make acceptance-contract
```

Disposable kind + Kueue Golden Path:

```bash
make e2e-golden
```

The project intentionally separates feature alignment from promotion evidence:
a new provider can implement the SPI before it is enabled, but it is not
supported until the centralized validation pass adds executable evidence and an
accepted capability-matrix row.

## Canonical docs

- [Go architecture](docs/CONTROL_PLANE_V2_GO.md)
- [Stage B provider SPI](docs/PROVIDER_ADAPTER_SPI.md)
- [Provider recovery contract](docs/PROVIDER_RECOVERY_CONTRACT.md)
- [Capability / release matrix](docs/CAPABILITY_RELEASE_MATRIX.md)
- [Workload lifecycle](docs/WORKLOAD_LIFECYCLE.md)
- [Stage B provider conformance](docs/STAGE_B_PROVIDER_CONFORMANCE.md)
