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

Use Go 1.27+ and run:

```bash
make fmt-check
make vet
make test
make acceptance-contract
make build
```

The full CI also builds container targets and runs the disposable kind + Kueue
Golden Path. PostgreSQL contract tests require `TEST_POSTGRES_DSN`.

**Shared PostgreSQL test isolation:** `make test` always runs `go test -p 1 ./...`
because multiple package fixtures currently share `TEST_POSTGRES_DSN`, and
`internal/store/postgres` resets tables with `TRUNCATE`. Running `go test ./...`
directly with that environment set may interleave package tests and produce
nondeterministic failures. Intra-package concurrency tests still run.
The Go Control Plane CI job provisions two disposable databases within its
PostgreSQL service: `gpu_platform_test` for Management and `gpu_store_test`
for Store contract tests. It sets `TEST_POSTGRES_STORE_DSN` and runs
`make test-parallel-isolated` (`go test -p 4 ./...`). Store fixtures refuse an
explicit override pointing to the same database name as Management.
Do not use this opt-in target on shared or production databases. The local
`make test` default stays serialized when a developer has only one test DB.
The package-isolation check verifies distinct database identities. CI must
pass at the exact commit before relying on parallel test acceptance.

For lifecycle fixes, include a regression at the failed boundary and describe the
trigger, observable result, and recovery semantics in the pull request.

## v0.1 scope

Keep HAMi/DRA/MIG integration, additional providers, and multi-cluster placement
outside the frozen v0.1 lifecycle unless they are introduced as separately reviewed
follow-up slices.

## License

Contributions are distributed under [Apache License 2.0](LICENSE).
