# Provider side-effect recovery contract

Status: P1, following the frozen v0.1 Golden Lifecycle baseline.

## Delivery semantics

Agent reconciliation is **at least once**. A lease fences control-plane publication,
but it cannot make an external provider write exactly once. The agent may crash after
a provider accepts a write and before the observation is durably reported.

Therefore every provider adapter must make replay safe by contract:

1. Project a deterministic provider identity from the desired resource identity.
2. Stamp the desired generation on provider-owned execution objects.
3. Reconcile the same identity and same generation idempotently.
4. Observe provider state independently after apply; never treat an apply ACK as proof
   that the desired state is durable or ready.
5. Fail closed when an immutable execution object exists for a different generation.
   Replacement requires an explicit provider policy; it must not be hidden inside retry.
6. Delete by deterministic identity and treat already-gone as success.
7. Report evidence only under the currently valid reconcile lease. A stale worker may
   have already caused a provider side effect, but it cannot publish or finalize state.

## Crash / takeover sequence

```text
worker A claim(epoch=N)
  -> provider apply succeeds
  -> A crashes before Report

lease expires
worker B claim(epoch=N+1)
  -> re-project same provider identity
  -> apply same generation (idempotent no-op/update)
  -> independently observe provider state
  -> Report under epoch=N+1
  -> release
```

The accepted outcome is one logical provider resource and one current observation.
The system does not claim exactly-once provider execution.

## Kueue provider v0.1

- ComputePool resources use deterministic ResourceFlavor / ClusterQueue / LocalQueue
  identities and create-or-update reconciliation.
- Workloads use deterministic Job identity.
- Jobs carry `ai.compute/generation`.
- Replaying an existing Job at the same generation is a no-op and preserves
  controller-owned status.
- A different desired generation against the same immutable Job fails closed until an
  explicit replacement policy is implemented.
- Workload observation is a separate read after apply.
- Deletion is idempotent and NotFound means gone.

## Acceptance

Unit/contract tests must cover same-generation replay without duplicate resources and
generation mismatch fail-closed behavior. The next chaos acceptance gate must exercise
crash-after-apply followed by lease expiry/takeover and successful observation by the
new owner.
