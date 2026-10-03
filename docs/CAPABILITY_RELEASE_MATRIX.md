# Capability and Release Matrix

This document separates **release-supported capabilities** from **upstream signals we are watching**.

The rule is intentionally conservative:

> Freeze the v0.1 Golden Path first. A scheduler, accelerator allocation mode,
> serving stack, or runtime feature becomes a supported capability only after it
> has an explicit provider contract and an acceptance row here.

## v0.1 frozen baseline

| Layer | Accepted combination | Status | Evidence |
| --- | --- | --- | --- |
| Kubernetes | kind node `v1.34.0` | supported in v0.1 | `scripts/e2e/go-kind-kueue-golden.sh` |
| Scheduler / queueing | Kueue `v0.19.6` | supported in v0.1 | real kind/Kueue Golden Path |
| GPU facts | Run:ai Fake GPU Operator `0.2.0`, H100 profile | supported in v0.1 acceptance | fake-GPU convergence + portable `h100-80g` binding |
| Allocation | extended resource `nvidia.com/gpu` | supported in v0.1 | Job projection + observation/evidence |
| Control plane | PostgreSQL desired/observed state, generation, lease, tombstone | supported in v0.1 | `make acceptance-contract` |
| Provider path | Kubernetes + Kueue | **only execution path in v0.1** | provider recovery contract |

The v0.1 release claim does **not** include DRA, HAMi/fractional GPU, KAI,
Volcano, KServe, llm-d, LWS, or SGLang role switching.

## Capability-driven rule

Cluster Capability Registration is the only discovery boundary between cluster
implementation details and the management plane.

Current v0.1 facts are deliberately small:

- Kubernetes version
- Kueue available
- portable accelerator classes observed on nodes
- Agent version and heartbeat

Future stages may extend the registered facts with fields such as scheduler
name/version, DRA support, fractional-GPU support, serving-provider features,
and supported accelerator classes. Those fields are **facts**, not scheduling
policy.

The control plane may use registered facts for admission, compatibility checks,
placement eligibility, and UX. It must not reproduce KAI, Volcano, Kueue,
KServe, llm-d, DRA, or accelerator-provider scheduling/allocation semantics.

## Provider adapter rule

Kueue is the only v0.1 execution provider.

Future integrations enter through optional provider adapters and must map back
to the same portable intent and lifecycle contracts:

```text
Portable intent
    -> ClusterBinding / provider selection
    -> Provider adapter projection
    -> generation-aware provider object
    -> observation + evidence
    -> lease-fenced commit
    -> delete / finalize / tombstone
```

A provider is not allowed to create a parallel source of truth or a second
recovery model.

## Watch-only upstream matrix

These rows are **signals to preserve in compatibility planning**, not acceptance
claims.

| Upstream | Version observed | Relevant changes | Current platform action |
| --- | --- | --- | --- |
| KAI Scheduler | `0.18.2` | fractional GPU memory/fraction limits; NRI/fractional runtime-class interaction fix; nvFraction-aware node scaling; default PodGroup behavior when Karta lacks gang instructions | record only; no KAI provider or HAMi/fractional acceptance in v0.1 |
| Volcano | `1.15.3` | DRA aggregate-device-count overflow fix; stale PodGroup annotation update fix; not-ready placeholder-node snapshot fix | record only; no Volcano or real-DRA acceptance in v0.1 |
| LWS | later stage | multi-replica naming stability is relevant to leader/worker identity | observe until Scheduler/Serving stages |
| KServe | `0.21+` target | serving provider candidate; resource claims, Canary, autoscaling integration are capability facts | Stage C only |
| llm-d | `0.10` target | serving/router baseline and supply-chain reference | Stage C only |
| SGLang | runtime role-switch capable builds | P/D role switch has drain/rebuild/failure semantics | Stage D only; no platform intent field yet |

Do not chase every release candidate. Update this matrix only when a combination
is either:

1. promoted into an acceptance gate, or
2. important enough to affect the design of a future provider contract.

## Evolution stages

### Stage A — v0.1 closeout (frozen)

- freeze lifecycle race, tombstone atomicity, lease fencing, recovery, and
  capability-registration evidence;
- keep Fake GPU + Kueue stable;
- keep DRA/HAMi/KAI/Volcano/Serving execution paths deferred;
- record upstream scheduler fixes here without changing the supported surface.

Frozen baseline: deterministic acceptance contracts and the real kind/Kueue
Golden Path remain green with capability registration included. Stage A changes
are limited to gate hardening, bug fixes, and watch-only upstream signal
updates; they must not add a new scheduler, accelerator allocation mode, or
serving execution path.

### Stage B — optional scheduler paths

Stage B starts only after the frozen Stage A gates remain green. The entry work
is deliberately split from provider implementation:

1. extend Cluster Capability Registration with scheduler name/version, DRA
   support, and accelerator classes as reported facts only;
2. define a `ClusterBinding -> provider adapter` SPI aligned with the existing
   ensure/materialize/observe/finalize lifecycle boundary;
3. select exactly one optional scheduler path (KAI or Volcano) and write its
   recovery contract first, reusing generation, lease fencing, and tombstones;
4. run a real-GPU pilot and write observation/evidence back through the existing
   evidence model before any new acceptance row is promoted.

Fractional/HAMi coexistence, DRA execution, and serving integrations remain out
of scope until a chosen Stage B provider has contract + real-GPU evidence.

Kueue remains a valid peer provider; KAI/Volcano do not replace the control
plane's lifecycle model.

### Stage C — serving provider

- introduce KServe 0.21+ as an optional Serving provider;
- align llm-d 0.10 images/router baseline and remove stale connector references;
- expose DRA-claim, Canary, and direct autoscaling support as capability facts;
- keep CRD manipulation inside the Cluster Agent/provider adapter.

The management plane declares intent; it does not become a KServe/llm-d
controller implementation.

### Stage D — elastic inference runtime

SGLang role switching remains a runtime capability observation until there is a
measured TTFT/TPOT pressure that justifies dynamic P:D intent.

Any future role switch must be modeled as a recoverable transaction:

```text
drain requests
    -> freeze generation/lease ownership
    -> transfer or invalidate KV ownership
    -> teardown communication resources
    -> rebuild role-specific runtime state / CUDA graphs
    -> synchronize router state
    -> verify
    -> commit observation
```

Failure must map to the existing generation + lease + tombstone/recovery model,
rather than adding an unrelated runtime state machine.

## Promotion rule

A deferred capability becomes supported only when all of the following exist:

- explicit capability-registration facts;
- provider adapter boundary;
- generation-aware deterministic identity;
- lease-fenced observation/finalization;
- replay-safe deletion/recovery;
- supply-chain-pinned production images;
- a named acceptance matrix row and executable Golden Path evidence.
