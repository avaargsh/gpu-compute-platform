# Stage B Volcano Provider Recovery Contract

Status: **falsification target only — not a supported provider**.

This document selects Volcano as the first optional scheduler/provider contract
to falsify after the v0.1 Kueue path. It does **not** register a Volcano adapter,
promote Volcano into `ClusterCapabilities.providers`, or add Volcano to the
release-supported matrix.

## Why Volcano first

The purpose of the second-provider experiment is to test whether the existing
provider-neutral lifecycle actually survives a scheduler with materially
different queue/gang semantics.

Volcano is a useful falsification target because it exposes explicit Queue,
VolcanoJob, and PodGroup objects while still letting the platform keep
scheduling semantics inside the provider.

The initial experiment is pinned to **Volcano >= v1.15.2**. Earlier v1.15
releases contained a DRA capacity-accounting vulnerability/failure mode that is
not an acceptable experimental baseline.

KAI remains watch-only for this experiment. Its NvFractions/GPU-sharing surface
is evolving quickly and its DRA whole-GPU/MIG/fractional work is still an active
roadmap area. Starting there would mix provider-neutral recovery validation with
fractional-GPU product design.

## Experiment boundary

The first Volcano slice deliberately supports only:

- Kubernetes + Volcano;
- whole-GPU portable accelerator bindings;
- ordinary extended resources such as `nvidia.com/gpu`;
- one ComputePool -> one deterministic Volcano Queue projection;
- one Workload -> one deterministic VolcanoJob projection;
- Volcano-created PodGroup/Pods as observed provider children.

It deliberately excludes:

- DRA execution;
- HAMi/vGPU/fractional GPU;
- MIG-specific allocation;
- topology-aware provider policy;
- inference serving;
- multi-cluster placement;
- provider selection policy.

Those capabilities may be observed as cluster facts, but they are not part of
this contract.

## Projection model

```text
Platform ComputePool
  portable accelerator intent
  generation
        |
        v
Volcano provider adapter
        |
        +--> scheduling.volcano.sh/v1beta1 Queue
        |
Platform Workload
  immutable execution intent
  generation
        |
        v
Volcano provider adapter
        |
        +--> batch.volcano.sh/v1alpha1 Job
                schedulerName: volcano
                queue: <deterministic pool queue>
                minAvailable: <portable workload requirement>
                |
                +--> controller-owned PodGroup
                +--> controller-owned Pods
```

The adapter must not own Volcano's queueing, gang, preemption, reclaim, or
backfill algorithms. It only projects portable intent and observes results.

## Deterministic provider identity

Provider-side names must be derived deterministically from platform identity:

- Queue identity derives from ComputePool ID;
- VolcanoJob identity derives from Workload ID and project namespace;
- Kubernetes object names use a DNS-safe, length-bounded slug **plus the first
  64 bits of SHA-256 over the original platform ID**. Slug normalization alone
  is not sufficient: `a_b`, `a.b`, and `a-b` must not all map to one provider
  object. The entire original ID remains in the ownership annotations;
- the projector requires the explicitly bound `provider=volcano` and a positive
  desired generation; it must never infer provider identity from scheduler facts;
- every platform-owned provider object carries:
  - platform resource ID;
  - desired generation;
  - provider identity = `volcano`;
  - enough ownership metadata to distinguish same-generation replay from a
    foreign/conflicting object.

Controller-created PodGroups and Pods are not independent platform resources.
Their owner references and parent VolcanoJob identity are evidence inputs, not
new desired-state objects.

## Scope of the current projection proof

The pure `internal/provider/volcano` projector now tests generation, provider
identity, deterministic collision-resistant naming, and portable whole-GPU
projection. It does **not** create or adopt live Volcano objects. The future
adapter must independently compare ownership annotations, immutable workload
projection, and observed provider objects before adoption. A hash suffix alone
is not an authorization or ownership proof; hash collisions, foreign objects,
and tampered annotations must fail closed at the adapter boundary.

No Volcano provider is registered by the production Cluster Agent at this stage.

## Executable create-or-adopt classifier

The Stage B package now includes a pure classifier for the deterministic-object
recovery decision. It performs no Kubernetes writes and is not wired into the
production registry.

Given an expected projection and an observed object:

- missing object -> `create`;
- matching provider-owned identity + generation + immutable projected `spec` -> `adopt`;
- provider/resource/generation mismatch -> fail closed;
- same-generation image, queue, accelerator quantity, or other immutable spec
  drift -> fail closed;
- Kubernetes/runtime metadata such as UID, resourceVersion, status, and
  non-platform annotations/labels may differ without changing ownership.

The current comparison is intentionally strict. If a real Volcano API server
adds defaults to the desired `spec`, the future dynamic-client adapter must
introduce an explicit normalization rule proven by kind/Volcano tests; it must
not weaken adoption to "same name + same generation".

This classifier validates the decision boundary only. It does not yet prove a
lost-ACK remote create, AlreadyExists race, process takeover, deletion replay,
or stale lease-owner fencing.

## Executable ensure-object recovery loop

On top of the pure adoption classifier, Stage B now has an unregistered
`ensureProjectedObject` state machine with an injected client interface:

```text
GET deterministic identity
  |
  +-- exists -> strict classify -> adopt | conflict
  |
  +-- NotFound -> CREATE
                   |
                   +-- success -> create
                   +-- transport/lost ACK -> return error; next reconcile GETs
                   +-- AlreadyExists -> fresh GET -> strict classify
```

The tests prove:

- first create followed by replay performs exactly one logical create;
- create commits remotely but returns a timeout, then the next reconcile adopts
  the existing object without issuing a second create;
- GET -> CREATE `AlreadyExists` races re-read the object and adopt only an
  exact provider/generation/spec match;
- conflicting generation or immutable spec fails closed;
- an ambiguous GET error never falls through to blind CREATE.

This is still a contract harness. The client is fake/injected and there is no
Volcano dynamic client, queue/job observation implementation, deletion path, or
production provider registration yet.

## Kubernetes dynamic transport proof

The next Stage B slice wires the unregistered ensure loop to a narrow
`dynamic.Interface` transport for exactly two GVRs:

- `scheduling.volcano.sh/v1beta1/queues` (cluster-scoped);
- `batch.volcano.sh/v1alpha1/jobs` (namespaced).

The transport supports only `GET` and `CREATE`. Tests use the Kubernetes
dynamic fake client to prove native NotFound behavior, Queue/VolcanoJob
create-then-adopt replay, and fail-closed GVK/scope validation.

This still is **not** a registered `provider.Adapter`. Observation, deletion,
status translation, evidence, real Volcano CRDs, kind+Volcano acceptance and
real-GPU execution remain separate gates.

## Create-or-adopt

Every reconcile follows the existing provider recovery contract.

### Object absent

Create the deterministic provider object.

### Same identity already exists

Adopt only when the existing object proves the same:

- platform resource ID;
- provider identity;
- desired generation;
- immutable execution projection.

This is the lost-ACK recovery path.

### Conflicting object

Fail closed when deterministic name collision or ownership metadata indicates:

- another platform resource;
- another provider;
- another generation;
- a different immutable Workload projection.

Do not delete-and-recreate a conflicting object to make reconciliation appear
successful.

## Workload immutability

An active Workload remains immutable exactly as in the Kueue path.

A changed workload uses:

```text
Delete desired
  -> provider cleanup
  -> observe provider object gone
  -> Finalize
  -> tombstone
  -> Recreate at higher generation
```

The Volcano adapter must not introduce in-place mutation of a running
VolcanoJob as a second lifecycle.

## Lease and stale-writer fencing

The existing resource-scoped reconcile lease remains authoritative.

The provider adapter may perform the remote Volcano/Kubernetes API call, but:

- observation writes require the current unexpired lease owner;
- finalization requires the current unexpired lease owner;
- a previous Agent cannot commit status after lease takeover;
- lease expiry does not make an in-flight provider call atomic.

After ambiguous transport failure, the new owner must re-observe the
deterministic provider object and classify ownership before it reports success
or releases authority.

## Lost-ACK cases that must pass

The contract is not accepted until tests prove:

1. Volcano Queue create succeeds but client loses the response;
2. VolcanoJob create succeeds but client loses the response;
3. retry adopts the exact same-generation object without duplicate creation;
4. create race with the same identity converges by adoption;
5. create race with a different generation fails closed;
6. process takeover preserves the same provider object UID;
7. stale Agent reporting/finalization is rejected after lease replacement.

## Observation semantics

Provider-specific states must not be relabeled as stronger portable guarantees.

In particular:

- `Ready=True` requires ready/succeeded workload evidence;
- `Admitted=True` may be reported only when Volcano Job/PodGroup state proves
  the workload has entered Volcano's scheduling lifecycle;
- a Volcano Queue being `Open` is not sufficient to claim workload admission;
- `QuotaReserved=True` must **not** be invented merely to resemble Kueue.
  If Volcano does not expose an equivalent reservation fact in this slice, the
  adapter leaves that condition false/absent with a provider-specific reason.

Evidence refs should include the VolcanoJob and, when present, its PodGroup and
relevant Pod identities.

## Deletion contract

### Workload

1. delete the deterministic VolcanoJob;
2. continue observing until the VolcanoJob is gone;
3. verify controller-owned PodGroup/Pods no longer represent live execution;
4. report provider cleanup complete;
5. only then allow Desired finalization/tombstone commit.

### ComputePool

Queue cleanup is dependency-gated behind Workload finalization.

A Queue must not be deleted if it is shared or ownership cannot be proven.
Cluster-scoped provider resources follow the same conservative rule already
used for shared Kueue ResourceFlavor objects.

## Crash matrix

The experiment must inject at least:

- before provider create;
- after provider accepts create / before reply;
- after reply / before observation commit;
- after observation / before lease release;
- process death with live lease;
- takeover after lease expiry;
- deletion accepted / reply lost;
- final observation succeeds / finalization fails.

Every case must reduce to one of:

- safe retry;
- deterministic re-observe/reconcile;
- blocked ambiguity.

No case may require blind duplicate mutation.

## Acceptance sequence

### Contract proof

Before any production registration:

1. provider projection unit tests;
2. same-generation create/adopt tests;
3. conflicting-generation rejection;
4. deletion replay tests;
5. portable observation/status translation tests;\n6. fake/dynamic-client crash and takeover tests;
7. kind + Volcano Golden Path using whole-GPU-style extended-resource intent.

Passing this phase proves only the provider contract.

### Promotion proof

`volcano` may appear in the production Cluster Agent registry only after:

1. a pinned Volcano production image/version;
2. the full provider recovery matrix passes;
3. a real-GPU controlled pilot executes through the adapter;
4. observation/evidence is written through the existing platform model;
5. the capability matrix receives an explicit supported row.

Until then:

```json
{
  "providers": ["kueue"]
}
```

remains the only valid production provider capability claim.

## Kill criteria

Stop the Volcano adapter experiment instead of adding another abstraction if
any of the following is required:

1. a second desired-state store or lifecycle state machine;
2. provider-specific authority/lease semantics outside the existing contract;
3. blind recreate after ambiguous create/delete;
4. hot mutation of active Workload intent;
5. pretending Volcano exposes Kueue-equivalent reservation semantics;
6. management-plane implementation of Volcano queue/gang/preemption policy;
7. provider-specific fields leaking into the public portable Workload contract.

A failed falsification is useful evidence. The project does not need a second
provider merely to claim pluggability.
