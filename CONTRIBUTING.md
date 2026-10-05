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

Read [the provider recovery contract](docs/PROVIDER_RECOVERY_CONTRACT.md)
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

## Stage B scope

The v0.1 Kueue lifecycle remains the compatibility baseline. New provider work
must use the existing adapter SPI and recovery contract.

A second provider may be implemented behind the SPI, but it must remain
unregistered until the centralized validation pass proves its recovery and
real-hardware evidence. DRA execution, HAMi/fractional GPU, serving providers
and multi-cluster placement remain separate later slices.

## License

Contributions are distributed under [Apache License 2.0](LICENSE).
