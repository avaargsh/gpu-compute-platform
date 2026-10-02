# Contributing

The v0.1 control-plane mainline is `master`. Submit focused changes through pull
requests and keep the lifecycle invariants explicit in both code and tests.

## Control-plane invariants

Changes to reconciliation or provider execution must preserve:

- immutable desired generations and delete/recreate semantics;
- deterministic provider identity and same-generation idempotency;
- remote reconcile lease fencing for stale writers;
- at-least-once reconciliation rather than exactly-once side-effect claims;
- independent observation after provider mutation;
- durable deletion/finalization evidence and tombstone fencing;
- fail-closed behavior when ownership or generation is ambiguous.

Read [the provider recovery contract](docs/provider-side-effect-recovery.md)
before changing provider side-effect semantics.

## Validation

Use Go 1.24+ and run:

```bash
make fmt-check
make vet
make test
make acceptance-contract
make build
```

The full CI also builds container targets and runs the disposable kind + Kueue
Golden Path. PostgreSQL contract tests require `TEST_POSTGRES_DSN`.

For lifecycle fixes, include a regression at the failed boundary and describe the
trigger, observable result, and recovery semantics in the pull request.

## v0.1 scope

Keep HAMi/DRA/MIG integration, additional providers, and multi-cluster placement
outside the frozen v0.1 lifecycle unless they are introduced as separately reviewed
follow-up slices.

## License

Contributions are distributed under [Apache License 2.0](LICENSE).
