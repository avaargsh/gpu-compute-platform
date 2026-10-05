# Capability and Release Matrix

This document separates **implemented product capability**, **accepted execution
capability**, and **watch-only upstream signals**.

A feature can exist in code without being promoted into the accepted execution
surface.

## Accepted baseline

| Layer | Accepted combination | Status | Evidence |
| --- | --- | --- | --- |
| Kubernetes | kind node `v1.34.0` | accepted baseline | `scripts/e2e/go-kind-kueue-golden.sh` |
| Scheduler / queueing | Kueue `v0.19.6` | accepted | kind/Kueue Golden Path |
| GPU facts | Run:ai Fake GPU Operator `0.2.0`, H100 profile | acceptance fixture | portable `h100-80g` binding |
| Allocation | extended resource `nvidia.com/gpu` | accepted baseline | Job projection + observation/evidence |
| Control plane | PostgreSQL desired/observed state, generation, lease, tombstone | accepted baseline | `make acceptance-contract` |
| Provider adapter | `kueue` | **only registered production adapter** | provider recovery contract + Golden Path |

The accepted claim does **not** include DRA execution, HAMi/fractional GPU,
KAI, Volcano, KServe, llm-d, LWS, or SGLang role switching.

## Stage B implemented surface

These capabilities now exist in mainline code but do not by themselves promote
another execution provider:

- scheduler facts: `schedulers[].{name, version?}`;
- DRA API fact: `draApiAvailable` + `draApiVersion`;
- portable accelerator inventory: `accelerators[]`;
- Agent execution capability: `providerAdapters[]`;
- immutable `ClusterBinding.clusterId`;
- immutable `ClusterBinding.provider`;
- provider identity copied into desired ComputePool / Workload state;
- `provider.Adapter` SPI;
- API rejection of providers outside the product provider catalog;
- Runtime fail-closed behavior for missing/unregistered adapters.

### Fact separation

```text
schedulers[]
    = software observed in the cluster

providerAdapters[]
    = adapters registered in the Cluster Agent

ClusterBinding.provider
    = immutable adapter selected for one pool
```

These are intentionally not interchangeable.

Observing a scheduler does not register an adapter. Registering an adapter does
not prove the scheduler is installed. A binding never causes the Runtime to
guess a replacement provider.

## Capability-driven rule

Cluster Capability Registration is the discovery boundary between cluster facts
and the management plane.

Capability fields are facts, not scheduling policy. The management plane may
use them for compatibility/preflight/UX, but must not reproduce scheduler,
device-allocation or serving-provider algorithms.

DRA API availability means only that the Kubernetes API is visible. It is not
evidence of a DRA driver, DeviceClass, compatible accelerator or accepted DRA
allocation path.

## Provider adapter rule

Every execution provider must map to the same lifecycle:

```text
Portable intent
    -> immutable ClusterBinding.provider
    -> desired provider identity
    -> provider.Adapter
    -> deterministic provider objects
    -> independent observation + evidence
    -> lease-fenced commit
    -> delete / finalize / tombstone
```

A provider may not create a parallel source of truth or recovery model.

Implementation and promotion are separate. A candidate adapter may be developed
behind the SPI while remaining absent from both the product provider catalog
and the production Runtime registry.

See [STAGE_B_PROVIDER_CONFORMANCE.md](STAGE_B_PROVIDER_CONFORMANCE.md).

## Watch-only upstream matrix

These rows are planning signals, not acceptance claims.

| Upstream | Version observed | Why it matters | Current action |
| --- | --- | --- | --- |
| KAI Scheduler | `0.18.2` | GPU-aware scheduling / fractional GPU semantics | candidate research only; not registered |
| Volcano | `1.15.3` | batch/gang scheduling and DRA-related fixes | candidate research only; not registered |
| Kubernetes DRA | API discovery only | future device-allocation path | report API fact only; no execution |
| HAMi | later slice | fractional GPU compatibility | defer until scheduler-provider contract is proven |
| LWS | later stage | multi-replica identity | Stage C+ research |
| KServe | `0.21+` target | serving-provider candidate | Stage C only |
| llm-d | `0.10` target | serving/router baseline | Stage C only |
| SGLang | role-switch capable builds | elastic inference runtime | Stage D only |

Update this table only when upstream changes alter a provider contract or a
combination is promoted into acceptance.

## Evolution stages

### Stage A — frozen baseline

Completed and frozen:

- lifecycle race/tombstone atomicity;
- lease fencing and takeover;
- at-least-once provider recovery;
- capability registration baseline;
- Kueue + fake-GPU Golden Path.

Stage A receives only compatibility fixes and gate hardening.

### Stage B — optional scheduler provider

**Completed entry work:**

1. scheduler/DRA/accelerator capability facts;
2. provider identity invariant;
3. provider adapter SPI;
4. provider catalog + Agent `providerAdapters[]` distinction.

**Current implementation work:**

5. choose exactly one candidate (KAI or Volcano);
6. implement its adapter against
   [STAGE_B_PROVIDER_CONFORMANCE.md](STAGE_B_PROVIDER_CONFORMANCE.md);
7. keep the candidate unregistered while functionality is aligned.

**Later centralized validation:**

8. run recovery/chaos/conformance acceptance;
9. run a real-GPU pilot;
10. promote only if evidence is sufficient.

The Kueue package already has internal ResourceClaim projection primitives, but
the public ComputePool API rejects `allocationMode=dra`; no DRA execution path
is promoted or accepted. HAMi/fractional GPU and serving remain out of scope for
this Stage B provider slice.

### Stage C — serving provider

Future only:

- define portable ModelRevision / ServingConfig / Deployment / Endpoint boundary;
- introduce one serving provider behind its own contract;
- keep CRD manipulation inside the Cluster Agent/provider implementation.

No serving provider is currently part of the supported product surface.

### Stage D — elastic inference runtime

Future only. Any P/D or role-switch operation must reuse generation, lease,
evidence and recovery semantics rather than create an unrelated state machine.

## Promotion rule

A candidate becomes a supported provider only when all are true:

- explicit capability facts required by the provider;
- implementation behind `provider.Adapter`;
- product provider catalog entry;
- Cluster Agent Runtime registration;
- deterministic generation-aware identity;
- lease-fenced observation/finalization;
- replay-safe deletion/recovery;
- supply-chain-pinned production images where applicable;
- named acceptance row;
- centralized executable recovery evidence;
- real-GPU evidence for the promoted path.

Catalog/registry changes are the **last** promotion step, not the first.
