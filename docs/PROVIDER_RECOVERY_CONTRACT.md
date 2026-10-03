# Provider Recovery Contract

This document defines the failure/recovery contract between the management-plane
desired state, the Cluster Agent, and execution providers.

The control plane deliberately does **not** claim exactly-once provider side
effects. Reconciliation is at-least-once. Correctness comes from deterministic
provider identity, generation fencing, lease-owned commits, idempotent
observation, and convergence.

## Boundary

```text
PostgreSQL desired state
        |
        | generation
        v
Cluster Agent
        |
        | resource-scoped reconcile lease
        v
Provider Adapter
        |
        | deterministic identity
        v
Kubernetes / Kueue / accelerator provider
        |
        | observe existing state
        v
Observation + evidence
        |
        | lease-owner + generation fence
        v
PostgreSQL
```

The lease limits concurrent reconcilers. It does not make a remote API call
atomic with lease expiry.

A legal execution window is therefore:

```text
Agent A owns lease
    |
    | CREATE Job
    v
Kubernetes side effect succeeds
    |
    | response is lost / Agent pauses / lease expires
    v
Agent B takes ownership
    |
    | observe deterministic provider identity
    v
adopt same-generation object
    |
    v
report current observation
```

The platform must converge without creating a second logical resource.

## Required provider invariants

Every provider that creates workload-owned execution objects must satisfy these
rules.

### 0. Provider identity is frozen with desired state

`ClusterBinding.Provider` selects the adapter, but it is not a live routing
switch. Once a pool binding exists, changing its provider in place is rejected.
The selected provider is copied into ComputePool and Workload desired specs and
therefore travels with the resource generation through reconcile, retry, delete,
and takeover.

```text
ClusterBinding(provider=kueue)
        |
        v
Desired generation N { provider: kueue }
        |
        +-- reconcile -> kueue adapter
        +-- retry     -> kueue adapter
        +-- delete    -> kueue adapter
        +-- takeover  -> kueue adapter
```

A future provider change must use an explicit migration or delete/recreate
contract. It must never reinterpret an active generation through another
adapter. Desired objects created before this field existed use the frozen v0.1
compatibility default `kueue`.

Unknown provider names fail closed and do not fall back to another registered
adapter.

### 1. Stable logical identity

The provider object name or idempotency key is derived from the platform
resource identity, not from the Agent process or reconcile attempt.

For the Kueue/Kubernetes provider:

```text
Workload train-1
  -> Job job-train-1

DRA Workload train-1
  -> ResourceClaim accelerator-train-1
```

Retries and takeover owners address the same provider object.

### 2. Provider generation marker

A provider object created for a desired resource carries the desired generation.

Current Kubernetes objects use:

```text
ai.compute/generation=<generation>
```

An existing object is adoptable only when its generation equals the current
desired generation. Generation drift fails closed.

### 3. Create-or-adopt semantics

A provider create follows:

```text
GET deterministic identity
  |
  +-- exists, generation matches -> adopt
  |
  +-- exists, generation differs -> fail closed
  |
  +-- missing -> CREATE
                  |
                  +-- success -> continue
                  |
                  +-- AlreadyExists -> re-GET
                                         |
                                         +-- generation matches -> adopt
                                         +-- generation differs -> fail closed
```

The `AlreadyExists` branch is not treated as a provider failure by itself. It
is evidence that another reconciliation may have completed the same
deterministic side effect in the GET-to-CREATE race window.

### 4. Lost acknowledgements replay safely

A remote create may succeed while the caller receives a timeout or loses the
response. The first reconciliation may therefore report a retryable error even
though the provider object exists.

The next reconciliation must observe the existing same-generation object and
must not issue another logical create.

### 5. Old owners cannot commit control-plane truth

Provider side effects and management-plane commits are separate boundaries.

After lease ownership moves, an old Agent may still complete a remote provider
request. It must not be allowed to commit:

- Observation
- Evidence
- Finalization

PostgreSQL validates the current unexpired lease owner in the same transaction
as observation/finalization writes.

### 6. Delete is observe-until-gone

Deletion is replay-safe and convergent:

```text
DELETE provider object
        |
        v
observe Gone?
  |          |
  no         yes
  |           |
retry      report final deletion evidence
              |
              v
          finalize desired
              |
              v
           tombstone
```

A lost delete acknowledgement is safe because a repeated delete of an already
missing object converges to `Gone=true`.

### 7. Evidence follows ownership

Evidence references describe the provider objects that participated in the
current generation and cleanup lifecycle. A stale lease owner cannot publish
same-generation evidence after takeover.

Evidence is an audit pointer, not a claim that the referenced provider object
still exists.

## Failure matrix

| Failure | Required behavior |
| --- | --- |
| Agent crashes before provider call | next owner performs reconciliation |
| Provider create succeeds, response is lost | retry observes and adopts the same-generation object |
| Lease expires while create is in flight | new owner converges on deterministic identity; old owner cannot report |
| Two owners race GET -> CREATE | one create wins; the other adopts only if generation matches |
| Existing provider object has old generation | fail closed; never report current generation as converged |
| Observation write fails after side effect | replay provider observation and report again |
| Delete succeeds, response is lost | repeated delete observes the resource as gone |
| Final observation succeeds, finalize fails | replay delete/observation/finalize safely |
| Finalize succeeds, response is lost | tombstone makes owned finalize replay idempotent |

## Non-goals

This contract does not provide:

- distributed transactions across PostgreSQL and Kubernetes
- exactly-once remote side effects
- provider-wide leader election
- a second scheduler
- automatic adoption of arbitrary user-created provider resources

Provider adapters must be idempotent and generation-aware instead.

## Release proofs

The deterministic release suite includes:

- `TestApplyJobReplaysAfterLostCreateAckWithoutDuplicate`
- `TestApplyJobAdoptsSameGenerationAfterCreateRace`
- `TestApplyJobCreateRaceRejectsDifferentGeneration`
- `TestRunnerSkipsProviderWhenLeaseIsContended`
- `TestPostgresLeaseTakeoverFencesStaleReportAndFinalize`
- `TestRunnerDeletionReplaysAfterFinalObservationFailure`
- `TestRunnerDeletionReplaysAfterFinalizeFailure`

The kind/Kueue Golden Path separately proves stable Job UID across a real Agent
lease takeover.

Future providers (DRA, HAMi, vendor-specific accelerators, or other schedulers)
must fit this contract rather than introduce a parallel recovery model.

The Stage B provider SPI intentionally registers only Kueue at first. A second
adapter is promotable only after its deterministic identity, create/adopt,
observation, deletion, and recovery behavior satisfy this document.
