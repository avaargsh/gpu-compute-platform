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

Stage B now permits a **narrow, exact-valued defaulting allowlist** while
comparing every platform-projected field, including list shape and GPU resource
maps. All other extra spec fields are conflicts, even with the same generation.
The Stage B v1.15.3 reviewed API-server fixtures permit only:

- Queue `spec.parent=root`, `spec.reclaimable=true`, `spec.dequeueStrategy=traverse`, `spec.weight=1`;
- VolcanoJob `spec.maxRetry=3`;
- Job Pod template `dnsPolicy=ClusterFirst` and
  `terminationGracePeriodSeconds=30`.

Additional `spec` fields such as Pod `hostNetwork`, `nodeSelector`, or
container resource requests are **not** adoptable. The allowlist captures
Stage B test-fixture assumptions, not verified universal Volcano defaults.
The Queue values above were confirmed by pinned Volcano v1.15.3 on disposable kind; **Job/PodGroup and runtime promotion are separate unproven gates**. Version changes fail closed pending
review, not silently widened via generic map-subset comparison.

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

This is still a contract harness. The client is fake/injected at this layer.
A later slice adds a narrow Kubernetes dynamic transport plus independent
observation/deletion primitives, but there is still no registered production
Volcano provider.

## Kubernetes dynamic transport proof

The next Stage B slice wires the unregistered ensure loop to a narrow
`dynamic.Interface` transport for exactly two GVRs:

- `scheduling.volcano.sh/v1beta1/queues` (cluster-scoped);
- `batch.volcano.sh/v1alpha1/jobs` (namespaced).

That early transport slice supported only `GET` and `CREATE`; the later
unregistered slices add conditional DELETE and Queue-only CAS UPDATE. Tests use the Kubernetes
dynamic fake client to prove native NotFound behavior, Queue/VolcanoJob
create-then-adopt replay, and fail-closed GVK/scope validation.

This still is **not** a registered `provider.Adapter`.

## Independent observation and observe-until-gone deletion

The next Stage B slice extends the narrow dynamic transport with `DELETE` and
adds unregistered lifecycle primitives.

Observation is an independent provider read after mutation:

- GET the deterministic Queue/VolcanoJob identity;
- NotFound is reported as non-existence without creating anything;
- an existing object must pass the same strict
  provider/resource/generation/immutable-spec classifier used for adoption;
- VolcanoJob `status.state.phase` and Kubernetes UID are read as provider
  reality only after ownership is proven.

Deletion also closes the GET -> DELETE time-of-check/time-of-use window:
the pre-delete GET must return a non-empty Kubernetes UID and resourceVersion;
the transport issues DELETE with *both* values in
`metav1.DeleteOptions.Preconditions`. Missing metadata fails closed and a
replaced or updated object causes an API conflict instead of a name-only
destructive action. The fake dynamic-client test asserts these options, since
its object tracker does not enforce real API-server preconditions.

Deletion deliberately does **not** treat a successful DELETE response as final
convergence. A DELETE NotFound response is **also** re-observed before Gone,
because the original object may have disappeared and the deterministic name
may already belong to another object:

```text
GET deterministic identity
  |
  +-- NotFound -> Gone=true
  |
  +-- exists -> strict ownership/spec classify
                  |
                  +-- conflict -> fail closed; never DELETE
                  |
                  +-- owned -> DELETE
                                |
                                +-- NotFound -> Gone=true
                                +-- transport error -> return error
                                |                    next reconcile re-observes
                                |
                                +-- success -> fresh GET
                                               |
                                               +-- NotFound -> Gone=true
                                               +-- still exists -> Gone=false
```

The tests prove:

- independent observation does not mutate provider state;
- observation rejects conflicting generations;
- deletion refuses a deterministic-name collision/foreign object before any
  DELETE call;
- DELETE acknowledgement alone is insufficient for `Gone=true`;
- successful delete converges only after observed NotFound;
- delete-committed/lost-ACK replay converges from NotFound without a duplicate
  destructive call;
- an ambiguous pre-delete GET never falls through to blind DELETE;
- an object without server-issued UID/resourceVersion cannot be deleted;
- DELETE carries observed UID and resourceVersion as API-side preconditions;
- DELETE NotFound followed by same-name foreign replacement fails closed;
- the Kubernetes dynamic fake transport exercises GET/CREATE/DELETE
  request shape for the projected Volcano GVRs (without simulating the
  API server's atomic precondition enforcement).

Run the isolated Stage B proof with:

```bash
make stage-b-volcano-contract
```

This gate is intentionally separate from `make acceptance-contract`: Volcano
remains an unregistered falsification target. Full provider status translation,
evidence projection into the platform API, real Volcano CRDs, kind+Volcano
acceptance, process-takeover proof, and real-GPU execution remain separate gates.

## Standalone unregistered provider adapter (Stage B)

The CPU-only Stage B integration provides a standalone `NewProvider(dynamic.Interface)`
which implements the portable provider SPI but is **not** registered in the
production Cluster Runtime or advertised as an executable cluster capability.

- `ReconcilePool`: ensure deterministic Queue, then GET/verify its current
  ownership and generation before translating controller status.
- `ReconcileWorkload`: ensure deterministic VolcanoJob, then independently
  GET/verify its current ownership and generation. A mere `Running` phase
  does **not** imply `Ready=True`: Pod/PodGroup readiness evidence remains an
  explicit future contract. A terminal `Completed` phase can establish
  completion.
- `DeletePool` / `DeleteWorkload`: reuse the already-tested
  `deleteProjectedObject` primitive from PR #54, including UID +
  resourceVersion API preconditions and observed-NotFound convergence.
  There must be no parallel name-only deletion implementation.
- Retryable transport failures are classified for the existing Agent retry
  contract, but cluster-agent registration, kind+Volcano, lease-takeover
  integration and real-GPU acceptance are independent promotion gates.

This is an executable contract/falsification target only. The Kueue path
remains the only production execution path. Other overlapping old Stage B
draft PRs must be consolidated without reintroducing a weaker DELETE path.


## Disposable kind/API-server Queue CAS falsification

A live, CPU-only **Kubernetes API-server** test is now available as
`scripts/e2e/volcano-queue-apiserver-contract.sh`. It is deliberately not
part of v0.1 acceptance or the normal local unit-test gate: it mutates a
uniquely named, cluster-scoped Queue on a specifically authorized disposable
kind cluster.

Prerequisites: `kubectl`, `kind`, `jq`, a **local disposable kind cluster**
with a corresponding `kind-*` context, and an installed, version-pinned
Volcano Queue CRD. The preflight requires `kind get clusters` to enumerate
the named cluster and compares the chosen context's API-server endpoint and
CA against `kind get kubeconfig --name <cluster>`; a similarly named arbitrary
context cannot authorize mutation. No kubeconfig credentials are saved to
the evidence directory. The script
does **not** install/upgrade Volcano or require a GPU. Do not point this
experiment at any production cluster.

```bash
# Verify you are on the exact PR #56 revision and a clean checkout.
git fetch origin feat/stage-b-volcano-queue-cas-v2
git switch feat/stage-b-volcano-queue-cas-v2
git pull --ff-only

STAGE_B_EXPECTED_SHA="<40-hex-independently-reviewed-PR-head>" \
STAGE_B_KIND_CONTEXT="kind-stageb-volcano" \
STAGE_B_KIND_MUTATION_ACK=1 \
STAGE_B_EXPECTED_QUEUE_CRD_SPEC_SHA256="<64-hex-pinned-schema-digest>" \
bash scripts/e2e/volcano-queue-apiserver-contract.sh
```

The 40-hex commit SHA must come from independent review, **not** by
reading the local `HEAD` being tested (which would make the freeze check
vacuous). The 64-hex schema digest is a **reviewed release baseline**: compute it
from a trusted copy of the Queue CRD **as persisted after Kubernetes CRD
defaulting** (not from the same live cluster run being tested), using
`jq -S '.spec' pinned-queue-crd.json | sha256sum`. Record the Volcano
release/chart and baseline provenance alongside the pinned hash. The test
blocks before creating any resource if the installed Queue CRD schema differs.

This emits **versioned, reviewable** evidence under a unique directory
outside the repository:
- original CREATE request, first readback fenced to CREATE UID + probe nonce + owner/generation, and exact server default values;
- metadata concurrent-write injection via UID-bearing, resourceVersion-fenced `kubectl replace` (never name-only `kubectl annotate`), with a checked ACK;
- installed Queue CRD JSON, canonical CRD spec SHA256 and Kubernetes version JSON;
- a real stale-resourceVersion UPDATE rejected as `409 Conflict`;
- an intentionally stale UID/RV **conditional DELETE** rejected as `409 Conflict`, followed by a fresh GET proving the Queue survived;
- a new-GET-based quota CAS from 8 to 16 with the same resource UID; an incidental controller status write can cause a *new* 409, in which case the test retries **at most four times**, each with a new GET and a fresh UID/RV-fenced request after validating the same owner, generation 4 and quota 8;
- per-attempt resourceVersion/Conflict/PASS receipts (`fresh-cas-attempts.tsv`), with non-409 failure and exhausted retry budget blocking acceptance;
- final independent GET and cleanup NotFound evidence;
- per-file SHA256 of JSON receipts and per-gate status.

The test fails closed if live server defaults disagree with the current
reviewed comparator (notably `reclaimable` and `dequeueStrategy`), if any
CAS/identity step is ambiguous, if the source generation/quota changes
unexpectedly during a retry, or if cleanup cannot be independently proved.
This bounded **test-fixture** retry does not change the production Provider's
409 handling or claim exactly-once effects.
On successful CREATE, the test records the API-server-issued **UID from
the CREATE response**. **Before the first post-CREATE mutation**, the fresh
GET must match that UID, probe token, owner, generation and quota. The
resourceVersion-fenced metadata update refuses concurrent replacements; a
new Queue sharing the old name cannot be annotated by accident. Before
cleanup, a fresh GET must match that original
UID, the unique test nonce, and a nonempty live resourceVersion. Cleanup then
uses raw Kubernetes `DeleteOptions` with those observed UID and RV
preconditions. Unlike `kubectl delete queues/<name>`, this enforces atomic
identity at the API server; replaced/updated resources return `409` and are
not silently deleted. A final GET must observe NotFound before PASS.

If CREATE returns an ambiguous/lost ACK or no server UID, **automatic
cleanup is blocked**: the test may leave an isolated Queue requiring manual
investigation. A matching name or token alone never authorizes deletion of
an unknown UID. Inspect the persisted probe evidence and perform deliberate
operator cleanup only after independently proving ownership.

**This tests Kubernetes object CAS and defaulting, not the actual Volcano
scheduler applying quota, lease takeover, multi-agent fencing or GPU runtime
behavior.** `QuotaApplied` remains `Unknown`. Earlier pinned
disposable kind runs include [full passing evidence #37919203307](https://github.com/avaargsh/temp-runner/actions/runs/37919203307)
and [a legitimate fresh-CAS 409 under controller status concurrency #38007371410](https://github.com/avaargsh/temp-runner/actions/runs/38007371410).
Each changed exact-head script requires an independent fresh kind re-run before acceptance.

## External Volcano Queue API findings (2026-10-08)

The official Volcano Queue documentation at
https://volcano.sh/docs/concepts/queue/ describes `status.state=Open` as
**available to receive PodGroups**, not evidence that an updated GPU
`spec.capability` quota has been applied by the scheduler. The upstream
`QueueStatus` API type at
https://github.com/volcano-sh/apis/blob/master/pkg/apis/scheduling/v1beta1/types.go
does not expose `observedGeneration` for this purpose.

Consequently, the Stage B unregistered adapter reports:
- `ObservedGeneration`: only the generation annotation verified on a **fresh
  Kubernetes GET**, not scheduler applied/reconciled generation.
- `Ready=True` for `status.state=Open`: Queue admission availability only.
- `QuotaApplied=Unknown`: no scheduler/controller application receipt yet,
  including after an UPDATE reply and a same-generation retry/adopt.

Neither an UPDATE ACK, a fresh GET, nor an Open status may be promoted into
`QuotaApplied=True` without **independent scheduler/admission evidence**.
The additional condition is intentionally visible to Stage B consumers. It
is not a reason to register Volcano as a production adapter.

**Resolved from real API-server evidence (2026-10-09):** temp-runner
[run #37918508288](https://github.com/avaargsh/temp-runner/actions/runs/37918508288)
deployed pinned Volcano v1.15.3 in disposable kind and created a Queue.
Its preserved `queue-cas/observed-defaults.json` showed exactly:
`parent=root`, `reclaimable=true`, `dequeueStrategy=traverse`,
`weight=1`. The same run proved stale UPDATE and conditional DELETE
conflicts, and correctly blocked the old narrower comparator. The
classifier and the live comparator now accept **only these exact reviewed
defaults**; unrelated or alternative scheduling fields still fail closed.
This evidence does **not** establish PodGroup/scheduler quota application,
GPU execution or production provider readiness. A clean rerun at the new
exact SHA is mandatory before merging.

The conflict/replacement unit tests remain fake-client contract tests, not
Kubernetes atomic precondition or Volcano controller proof.

## Monotonic Queue-generation CAS (stacked Stage B experiment)

PR #56 is stacked on the safe, **unregistered** adapter of PR #55.
It replaces the older PR #52 approach rather than copying its name-only DELETE
or claiming that `Running` proves Workload readiness. Kueue is still the only
production provider.

When `ReconcilePool` observes a Queue at a **lower** generation:

1. Re-read by deterministic Queue identity. Require exact GVK, name, scope,
   provider, pool, accelerator class and a positive stored generation.
2. Prove that the old spec differs from the projected Queue only by the
   accelerator **quota** for the **same** extended-resource key. Reject extra
   resources, unreviewed spec/default values, unexpected `ai.compute/*`
   metadata and objects already terminating.
3. Require Kubernetes-issued nonempty `UID` and `resourceVersion`. Carry
   these on UPDATE, preserving third-party labels/annotations, ownerReferences,
   finalizers and reviewed Volcano defaults. Only change the quota and the
   `ai.compute/generation` value; never submit controller-owned `status`.
4. Treat a `409 Conflict` or ambiguous/lost UPDATE acknowledgement as a
   **retryable reconciliation error**, *not* an instruction to retry the same
   mutation. Agent backoff must start from a fresh GET and revalidate identity.
5. On successful UPDATE response, require the same UID, an **ACK
   resourceVersion that is nonempty and differs from the submitted CAS
   version**, and exact new projection. An empty or unchanged response RV
   makes the operation's acknowledgement ambiguous: report a retryable
   error and only reconcile again from a fresh GET (never blindly repeat
   the UPDATE). This is not a cryptographic ownership proof.
   Reconcile then independently GETs and validates the observed Queue **and
   matches its UID to the successful UPDATE response**. Equal names, generation,
   ownership annotations and quotas do not prove identity if a Queue was
   deleted and recreated between UPDATE and observation. A changed UID is an
   ownership conflict, not a successful reconciliation. If an UPDATE committed
   but ACK was lost, replay adopts an already-updated generation without issuing
   a duplicate UPDATE; that recovery confirms converged desired state, **not
   retroactive proof of the original lost-ACK operation's UID**.

A same-generation immutable-spec change and a rollback to an older generation
are always rejected. VolcanoJob remains immutable; its lifecycle uses the
previous create/adopt and conditional-DELETE code paths.

**Safety boundary:** resourceVersion provides a server-side optimistic
concurrency compare for UPDATE; fake clients are not proof of API-server atomic
behavior, real defaulting, lease takeover or post-update controller reality.
Actual kind+Volcano acceptance, a deliberate conflict/replacement experiment,
and independent local tests remain outstanding before this stacked draft can
be promoted. No real-GPU/Volcano-production claim is attached to these tests.

For local CPU-only gates when GitHub Actions minutes are exhausted, pin the
exact 40-character PR HEAD and execute the test suite in a **detached local
worktree** without mutating the developer's current checkout:

```bash
# Take the SHA from an independent PR review; do NOT derive the expected
# value from whatever HEAD happens to be in your current checkout.
export STAGE_B_EXPECTED_SHA="<reviewed 40-character PR #56 commit SHA>"
git fetch origin feat/stage-b-volcano-queue-cas-v2
git switch feat/stage-b-volcano-queue-cas-v2
bash scripts/stage-b-local-gate.sh --self-test
TEST_POSTGRES_DSN="postgres://<user>:<password>@localhost:5432/gpu_platform_test?sslmode=disable" \
bash scripts/stage-b-local-gate.sh
```

Replace the credentials with those for a dedicated local PostgreSQL `*_test` database.
The contract tests execute schema setup and `TRUNCATE`; the script now **rejects**
non-loopback PostgreSQL hosts, databases without the `_test` suffix, arbitrary
query options (including `?host=`, `?dbname=` and `?port=`), absent DSNs,
dirty checkouts and SHA mismatches. Only a reviewed `sslmode` parameter may be passed.
A `--self-test` runs nine DSN guard fixtures without Go/Postgres and is
also a mandatory logged gate in the full local acceptance run.
The full gate also refuses inherited `PGHOST`, `PGHOSTADDR`, `PGPORT`,
`PGDATABASE`, `PGSERVICE`, `PGSERVICEFILE` or `PGOPTIONS` settings.
Such environment-based overrides may redirect the connection, load external
service definitions or alter the SQL search path despite an apparently safe
DSN. Clear them explicitly before running tests. This defense is not a
substitute for validating that the local TCP port is not a tunnel or proxy
into a production database. A loopback host can still be a tunnel:
verify the actual database target before running. Never point this at production.
It uses a detached worktree, runs fmt, vet, focused Volcano tests, Volcano
race tests, full Go tests, acceptance contract, supply-chain checks and build,
and emits per-gate logs, SHA256s and a gate table in an external evidence
directory. A failed or skipped gate must not be labeled PASS. This is not an
independent-review receipt: a separate reviewer must inspect changed code,
test coverage and logs. It is not a real Volcano API-server or GPU acceptance.

The low-level focused suite remains `make stage-b-volcano-contract`; the local
gate incorporates this and additional checks on the same frozen SHA.
Do not merge until evidence is recorded. The authoritative production registry
still contains only `kueue`.

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
- a differing generation for an immutable VolcanoJob, or a Queue generation
  outside the strictly verified monotonic Queue CAS path described above;
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
5. fake/dynamic-client crash and takeover tests;
6. kind + Volcano Golden Path using whole-GPU-style extended-resource intent.

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
