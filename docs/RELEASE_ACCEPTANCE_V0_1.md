# v0.1 Release Acceptance

This document defines the release gate for the v0.1 Go control plane. A change is not release-ready merely because it compiles or because the happy-path workload starts. The platform must preserve control-plane ownership, provider idempotency, generation fencing, deletion safety, and audit evidence under replay and restart.

## Required gates

| Gate | Failure being prevented | Acceptance evidence |
| --- | --- | --- |
| Desired/observed generation | stale provider state reported as current | stale-generation tests + provider generation annotation |
| Reconcile lease | concurrent agents issuing duplicate side effects | lease ownership/expiry contract |
| Provider create replay | lost create ACK or GET→CREATE takeover race creating a second logical object | deterministic identity + create-or-adopt tests |
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

## Machine-bound lifecycle matrix

`make acceptance-contract` is intentionally pinned to named invariants rather
than being only an alias for the whole unit-test tree. This makes the v0.1
release claim auditable when unrelated tests are added or removed.

| Invariant | Contract proof |
| --- | --- |
| Agent restart replays the same desired identity | `TestRunnerRestartReplaysDesiredSafely` |
| Lost provider create ACK replays without a duplicate logical Job | `TestApplyJobReplaysAfterLostCreateAckWithoutDuplicate` |
| GET→CREATE takeover race adopts only the same generation | `TestApplyJobAdoptsSameGenerationAfterCreateRace`, `TestApplyJobCreateRaceRejectsDifferentGeneration` |
| Lease contention prevents duplicate provider writers | `TestRunnerSkipsProviderWhenLeaseIsContended` |
| Final observation/finalize failures are replay-safe | `TestRunnerDeletionReplaysAfterFinalObservationFailure`, `TestRunnerDeletionReplaysAfterFinalizeFailure` |
| Lease takeover fences stale writers | `TestPostgresLeaseTakeoverFencesStaleReportAndFinalize` |
| Repeated concurrent lease claims admit exactly one live owner | `TestPostgresLeaseClaimStressHasSingleLiveOwner` |
| Finalize atomically clears runtime state and writes tombstone | `TestPostgresFinalizeDesiredAtomicallyCleansRuntimeState` |
| Old generation cannot cross a tombstone; newer recreate can | `TestPostgresTombstoneFencesOldGenerationAndAllowsNewerRecreate` |
| PostgreSQL reconstruction preserves desired/observed/lease state | `TestPostgresStateSurvivesStoreReconstruction` |
| Restart preserves deletion intent and tombstone fence | `TestPostgresRestartPreservesDeletionIntentAndTombstoneFence` |
| Workload admission cannot cross a concurrent parent-pool delete | `TestPostgresCreateWorkloadDesiredRejectsConcurrentPoolDelete` |
| Lost-ACK workload admission replay remains idempotent | `TestPostgresCreateWorkloadDesiredPreservesLostAckReplay` |
| Owned finalize replay is idempotent after a lost ACK | `TestPostgresFinalizeDesiredOwnedIsIdempotentAfterLostAck` |
| Generic desired upsert cannot cancel active deletion | `TestPostgresUpsertCannotCancelDeletionLifecycle` |
| Repeated delete/upsert races preserve deletion timestamp and cleanup finalizer | `TestPostgresLifecycleRaceStressPreservesDeletionFence` |
| Recreate waits for committed finalization and consumes tombstone | `TestPostgresRecreateWaitsForFinalizationReceiptAndConsumesTombstone` |
| Legacy/current tombstone coexistence cannot shadow current desired identity | `TestPostgresOwnedFinalizePrefersCurrentDesiredOverOlderTombstone` |
| Public API requires delete/finalize/recreate for immutable workload changes | `TestResourceAPIRequiresDeleteRecreateForWorkloadChanges` |
| Public API rejects stale workload generation rollback | `TestResourceAPIRejectsStaleWorkloadGenerationWithoutRollback` |

If an invariant is renamed or replaced, the release gate and this table must move
in the same change. A green generic `go test ./...` run is not a substitute for
this named lifecycle proof.

The two stress contracts repeat their critical PostgreSQL races 20 times per test run. They use synchronized goroutine starts rather than random sleeps, so CI repeatedly exercises lock/serialization semantics without turning the release gate into a probabilistic soak test.

## v0.1 restart contract

### Cluster Agent restart

The Agent is disposable process state. After restart it pulls the same desired state and safely replays reconciliation. Provider operations therefore must remain idempotent for the same generation. A restarted Agent must not create a second Job or mutate the execution projection.

### Provider side-effect recovery

The provider boundary is explicitly at-least-once. A reconcile lease limits who
may begin work, but a remote Kubernetes request can complete after the caller
loses its response or after lease ownership moves.

Provider correctness therefore depends on deterministic object identity and
generation-aware adoption:

```text
GET stable provider identity
   -> missing
CREATE
   -> success                  -> continue
   -> response lost            -> retry later and observe existing object
   -> AlreadyExists race       -> re-GET
                                  -> same generation: adopt
                                  -> different generation: fail closed
```

The Kueue/Kubernetes provider binds workload identity to stable Job and
ResourceClaim names and annotates them with `ai.compute/generation`. It does not
create attempt-specific execution identities. This prevents lease takeover from
turning one logical workload into duplicate provider resources.

This does not make provider execution exactly-once. An old owner may still
finish an in-flight remote call; the control plane prevents that owner from
committing observation/evidence/finalization after takeover, and the next owner
converges by observing the deterministic provider identity.

See [PROVIDER_RECOVERY_CONTRACT.md](PROVIDER_RECOVERY_CONTRACT.md) for the full
provider contract.

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
5. Agent A's observation identifies its explicit reconcile owner.
6. while A's resource lease is live, concurrent Agent B cannot become the Workload observation writer.
7. killing Agent A does not implicitly erase the resource lease.
8. after lease expiry, Agent B takes ownership and the Kubernetes Job UID remains unchanged.
9. an identical same-generation PUT remains idempotent.
10. an in-place generation/spec replacement returns HTTP 409.
11. an invalid accelerator class fails closed without creating a Job.
12. deletion removes provider-owned Workload resources before finalization.
13. ComputePool cleanup waits for dependent Workloads and preserves shared ResourceFlavor.

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
