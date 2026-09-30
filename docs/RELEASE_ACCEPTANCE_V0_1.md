# v0.1 Release Acceptance

This document defines the release gate for the v0.1 Go control plane. A change is not release-ready merely because it compiles or because the happy-path workload starts. The platform must preserve control-plane ownership, provider idempotency, generation fencing, deletion safety, and audit evidence under replay and restart.

## Required gates

| Gate | Failure being prevented | Acceptance evidence |
| --- | --- | --- |
| Desired/observed generation | stale provider state reported as current | stale-generation tests + provider generation annotation |
| Reconcile lease | concurrent agents issuing duplicate side effects | lease ownership/expiry contract |
| Lease write fence | old owner reporting/finalizing after takeover | owner-bound observation/finalize tests |
| Retry/backoff | transient provider outage becoming terminal drift | retry/backoff contract |
| Agent restart | process restart losing convergence safety | new Runner replay contract + Golden Path restart |
| Control-plane restart | desired/observation/lease state lost with process memory | PostgreSQL store reconstruction contract |
| Evidence | successful state without provider proof | Job + Kueue evidence refs in Golden Path |
| Deletion replay | crash between provider cleanup, evidence report and finalize | final-observation/finalize replay contracts |
| Tombstone | deleted generation resurrected by stale write | tombstone fence/recreate contract |
| Workload replacement | immutable Job serving changed desired intent | public delete/finalize/recreate contract |
| Real scheduler path | unit tests diverging from Kubernetes/Kueue behavior | kind + Kueue + Fake GPU Golden Path |

## Acceptance command

The deterministic contract suite is:

```bash
make acceptance-contract
```

The real cluster gate remains:

```bash
make e2e-golden
```

CI must pass both before merge.

## v0.1 restart contract

### Cluster Agent restart

The Agent is disposable process state. After restart it pulls the same desired state and safely replays reconciliation. Provider operations therefore must remain idempotent for the same generation. A restarted Agent must not create a second Job or mutate the execution projection.

### Lease takeover and write fencing

The lease protects two separate boundaries:

```text
claim lease
   -> provider side effects
   -> report observation/evidence
   -> finalize desired state
   -> release/delete lease
```

A competing Agent is fenced before provider reconciliation while another
unexpired owner holds the lease. After expiry, a new Agent may take ownership.

Ownership is also checked again when observations and finalization are committed.
Those checks happen in the same store transaction as the write. Therefore an
Agent that started work under an older lease cannot commit same-generation
evidence or finalize the resource after another Agent has taken ownership.

The HTTP Agent API requires a lease owner on these writes. PostgreSQL is the
authoritative implementation; the in-memory store mirrors the same contract for
deterministic tests.

### Control Plane restart

PostgreSQL is the durable management-plane source of truth. Constructing a new Store over the same database must preserve:

- DesiredResource and generation
- Observation and evidence refs
- active reconcile lease ownership
- deletion tombstones

The v0.1 acceptance suite proves this at the store boundary. A future deployment-level HA test may additionally restart the HTTP process against the same external PostgreSQL instance.

## Golden Path assertions

The kind acceptance path must prove all of the following against real Kubernetes/Kueue objects:

1. H100 portable intent resolves to the configured provider resource and flavor.
2. Kueue reaches QuotaReserved and Admitted.
3. the Job carries `ai.compute/generation=1`.
4. status exposes both Job evidence and Kueue Workload evidence.
5. restarting the Cluster Agent keeps the same Kubernetes Job UID.
6. an identical same-generation PUT remains idempotent.
7. an in-place generation/spec replacement returns HTTP 409.
8. an invalid accelerator class fails closed without creating a Job.
9. deletion removes provider-owned Workload resources before finalization.
10. ComputePool cleanup waits for dependent Workloads and preserves shared ResourceFlavor.

## Release rule

v0.1 is releasable only when:

```text
fmt + vet + unit/integration tests
        +
release acceptance contracts
        +
kind/Kueue Golden Path
        = PASS
```

DRA, HAMi, multi-provider and advanced placement work must not weaken these gates. New providers fit the lifecycle contract; they do not create a parallel release model.
