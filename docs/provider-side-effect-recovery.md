# Provider side-effect recovery contract

Status: **v0.1 mainline contract**.

## Delivery semantics

Agent reconciliation is **at least once**. A control-plane lease fences publication,
but it cannot make an external provider write exactly once. A worker may crash after
the provider accepts a write and before observation is durably reported.

Every provider adapter therefore must:

1. Project a deterministic provider identity from desired resource identity.
2. Stamp the desired generation on provider-owned execution objects.
3. Reconcile the same identity and generation idempotently.
4. Observe provider state independently after apply; an apply ACK is not proof.
5. Fail closed when an immutable object exists for another generation.
6. Delete by deterministic identity and treat already-gone as success.
7. Publish observation/finalization only under the current reconcile lease.
8. Recover lost-ACK and create-race cases by adopting only provably owned,
   same-generation provider objects.

## Crash and takeover

```text
worker A claim(epoch=N)
  -> provider apply succeeds
  -> ACK/report is lost or A crashes

lease expires
worker B claim(epoch=N+1)
  -> re-project deterministic provider identity
  -> inspect existing provider object
  -> adopt only if ownership + generation match
  -> independently observe provider state
  -> report under epoch=N+1
  -> finalize/release
```

The accepted outcome is one logical provider resource plus one current observation.
The system does **not** claim exactly-once provider execution.

## Kueue provider v0.1

- ComputePool resources use deterministic ResourceFlavor / ClusterQueue / LocalQueue
  identities.
- Workloads use deterministic Job identity.
- Provider-owned execution objects carry desired-generation identity.
- Same-generation replay is idempotent.
- A conflicting generation fails closed until an explicit replacement policy exists.
- Observation is a separate read after mutation.
- Deletion is idempotent and NotFound means gone.

## Required proof

The release gate must cover:

- lost ACK replay without duplicate side effects;
- same-generation create-race adoption;
- conflicting-generation rejection;
- crash/takeover recovery under a newer lease epoch;
- stale writer publication/finalization rejection;
- restart-safe deletion and tombstone fencing.

Provider recovery is a control-plane contract, not an adapter implementation detail.
