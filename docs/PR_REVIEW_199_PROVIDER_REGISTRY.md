# PR #199 Review Evidence: Production Kueue-Only Provider Registry

**Review date:** 2026-10-24  
**Repository:** [avaargsh/gpu-compute-platform](https://github.com/avaargsh/gpu-compute-platform)  
**PR:** [#199 — test(stage-b): freeze production Kueue-only provider registry](https://github.com/avaargsh/gpu-compute-platform/pull/199)  
**Contributor:** `musuntana`  
**Reviewed head:** `a946de7d5ef0009ecebd63d1dfd980f0fdec0f35`  
**Squash merge:** `4aa99cbaac622bd2af47f59dbab7b3c7626db419`

## Decision

Merged after source-level review and successful exact-head PR CI. The change is a bounded test/documentation update; it does not promote Volcano or any additional provider to production support.

## Change summary

The PR modifies four files with 97 additions and no deletions:

- `Makefile`: adds the production registry regression test to `make acceptance-contract`.
- `docs/PROVIDER_ADAPTER_SPI.md` and `docs/RELEASE_ACCEPTANCE_V0_1.md`: document the provider-registration invariant and its proof.
- `internal/cluster/runtime_test.go`: adds regression coverage for the production registry and invalid provider dispatch.

The production-runtime test asserts that `NewRuntime` registers only `KueueProviderName` and that the `volcano` adapter is not dispatchable. The lifecycle negative test covers Pool and Workload reconcile/delete operations for both a missing provider identity and unregistered `volcano`, checks that calls fail with a provider-related error, and verifies the registered adapter receives no calls.

## CI evidence

Source: [Go Control Plane workflow run #40046140001](https://github.com/avaargsh/gpu-compute-platform/actions/runs/40046140001), requested by exact head SHA `a946de7d5ef0009ecebd63d1dfd980f0fdec0f35`.

- **Passed:** Go Control Plane job, including format check, `go vet ./...`, `make test-parallel-isolated` (the parallel Go package test suite), and binary builds.
- **Passed:** Go Portal / Vue contract build, including project/operations-triage contracts, typecheck/build, and Management browser-contract regressions.
- **Skipped:** `make acceptance-contract` and production supply-chain policy. The workflow only runs these on manual dispatch or version-tag events.
- **Skipped:** real Chromium-to-Go API and kind + Kueue Golden Path jobs.

Therefore this PR has passing code-level regression evidence, but this run does **not** prove real kind/Kueue acceptance, physical-GPU behavior, or production supply-chain acceptance. The PR itself also explicitly disclaims real-cluster and physical-GPU acceptance.

## Merge record and remaining boundary

GitHub accepted a squash merge with the expected PR head SHA pinned to the reviewed commit. Merge commit: [`4aa99cbaac622bd2af47f59dbab7b3c7626db419`](https://github.com/avaargsh/gpu-compute-platform/commit/4aa99cbaac622bd2af47f59dbab7b3c7626db419).

A separate workflow run on the merge commit was not available at the time this note was written. The pre-merge exact-head workflow is the cited CI evidence.

Future production provider registration must update this contract and provide provider-specific negative-case, recovery, and environment acceptance evidence. Scheduler discovery remains inventory; it is not permission to dispatch an unaccepted provider.
