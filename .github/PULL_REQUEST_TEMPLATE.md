## Summary

<!-- What changed, and why now? -->

## Lifecycle / provider invariant impact

- [ ] Desired generation / immutable identity
- [ ] Deterministic provider identity / same-generation adoption
- [ ] Reconcile lease fencing / stale-writer rejection
- [ ] At-least-once provider recovery semantics
- [ ] Independent observation / evidence
- [ ] Finalizer / tombstone / delete semantics
- [ ] Cluster capability facts / provider adapter boundary
- [ ] Supply-chain policy
- [ ] No lifecycle invariant changes

## Scope guard

- [ ] This change does not introduce a second scheduler or hidden placement policy.
- [ ] DRA / HAMi / MIG / additional provider behavior remains deferred unless this PR is explicitly scoped and evidenced for it.
- [ ] Portable accelerator intent remains separate from provider-specific resource names and flavors.

## Validation

Commands run:

```text
make fmt-check
make vet
make test
make acceptance-contract
make build
# add make e2e-golden when lifecycle/provider behavior is touched
```

Results:

```text
# paste concise result summary
```

## Recovery / failure behavior

<!-- Describe retry, crash/takeover, stale-owner, delete/finalize, or provider-side-effect implications. -->

## Risk and rollback

<!-- Blast radius and rollback/recovery path. -->
