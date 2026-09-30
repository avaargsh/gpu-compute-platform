# Contributing

The current control-plane work lives on `feat/control-plane-v2-go`.
Target focused changes at that branch. The legacy `master` and closed PR #2
are outside the current development scope.

Read [the architecture contract](docs/CONTROL_PLANE_V2_GO.md),
[deletion contract](docs/finalizer-deletion.md) and
[frozen baseline](docs/baselines/finalizer-deletion.md) before changing lifecycle
behavior. Preserve generation CAS, provider ownership and remote lease fencing.

## Validation

Use Go 1.24+ for `make fmt-check`, `make vet`, `make test` and `make build`.
Set `TEST_POSTGRES_DSN` to a disposable database to execute PostgreSQL contract
tests; without it those tests skip. They create schema and truncate test tables.

The full push CI also builds both Docker targets and runs `make e2e-golden`
with Docker, kind, kubectl and Helm. Golden Path uses simulated accelerators and
a disposable cluster; it is not a real GPU benchmark.

Explain the trigger, resulting behavior and relevant validation in a pull request.
For lifecycle fixes, include a regression that exercises the failed boundary.
Keep DRA/HAMi/MIG, new providers and Placement Migration outside this frozen slice.

## License

Contributions are distributed under [Apache License 2.0](LICENSE).
