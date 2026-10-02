# AI Compute Control Plane v2 — Go Architecture

## Product domain

```text
Tenant
└── Project
    ├── ComputePool
    ├── Workload
    └── Serving
```

Projects are management-plane objects. They are not owned by a Kubernetes cluster.
Placement is explicit through ProjectBinding and ClusterBinding.

## Runtime boundary

```text
Vue Console
    |
    | REST /api/v1
    v
Go Control Plane
    ^
    | registration / heartbeat / capabilities
    |
Cluster Agent
    |
    | pull desired state / report observation
    v
Kubernetes + Kueue + accelerator stack
    |
    | provider reconciliation
    v
Kubernetes + Kueue + accelerator stack
    |
    | observed state / evidence
    v
Go Control Plane
```

## Source of truth

PostgreSQL is the management-plane source of truth.

Kubernetes is an execution provider. Kubernetes resources are projections of platform desired state, not a second product source of truth.

## Core invariants

1. Tenant and Project define isolation and ownership.
2. ComputePool is the portable compute contract.
3. Workload and Serving consume ComputePool capacity.
4. Accelerator classes are portable product identifiers; providers translate them to concrete resources.
5. Scheduling is delegated to scheduler providers. The platform does not implement a scheduler.
6. Project placement and pool placement are explicit bindings.
7. Every reconciled resource carries desired generation and observed generation.
8. Conditions and evidence are first-class status.
9. The cluster agent owns downstream observation and provider reconciliation.
10. Cluster execution capabilities are discovered in the compute plane and persisted as control-plane facts at registration time.
11. Capability status records factual versions, capabilities and heartbeat timestamps; availability policy is evaluated separately rather than encoded into discovery.
12. The public API does not expose arbitrary Kubernetes CRUD as the product model.

## Initial Golden Path

```text
Tenant
  -> Project
  -> ProjectBinding
  -> ComputePool
  -> ClusterBinding
  -> Kueue resources
  -> Workload
  -> admitted
  -> Pods Ready
  -> ObservedState
```

Serving follows after the batch Golden Path is stable.

## Go repository shape

```text
cmd/
  control-plane/
  cluster-agent/

internal/
  domain/
  agent/
  platform/
    httpapi/
  provider/
    kubernetes/
    kueue/
  store/
    postgres/
```

The first implementation remains a modular monolith. Service decomposition is intentionally deferred.


## Cluster capability registration

The Cluster Agent discovers execution facts from its local Kubernetes cluster and
publishes them during registration:

```text
Kubernetes discovery
      |
      v
ClusterCapabilities
  - Kueue
  - Serving
  - accelerator classes
      |
      v
Agent Registration
      |
      v
PostgreSQL cluster_agents.capabilities
      |
      v
GET /api/v1/clusters/{clusterID}/status
```

This is deliberately an inventory of observed execution capabilities, not a
scheduler and not a second desired-state model. Future placement preflight may
consume these facts, but registration itself does not make placement decisions.

The control plane stores `lastHeartbeatAt` instead of materializing a permanent
`connected` boolean. Liveness thresholds depend on deployment policy and Agent
sync cadence and should be evaluated from the timestamp rather than hidden in
capability discovery.
