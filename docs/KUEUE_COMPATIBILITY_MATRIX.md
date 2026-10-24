# Kueue / Kubernetes Compatibility Matrix

**Review date:** 2026-10-24  
**Scope:** Stage B provider SPI and the single production Kueue execution path.  
**Status:** Fresh-cluster Golden Path results are version- and commit-specific; in-place Kueue upgrades remain unverified.

## Frozen v0.1 baseline

| Dimension | Frozen value | Evidence / constraint |
| --- | --- | --- |
| Kubernetes | kind node image `kindest/node:v1.34.0` | Existing real kind/Kueue Golden Path |
| Kueue | `v0.19.6` | `scripts/e2e/go-kind-kueue-golden.sh` default |
| Production provider registry | `providers: ["kueue"]` | `TestProductionRuntimeRegistersOnlyAcceptedKueueProvider` and `make acceptance-contract` |
| Admission authority | Kueue only | No MultiKueue, Volcano, KAI, or second scheduler is enabled by this matrix |

Do not change the frozen release claim merely because an upstream version is newer.

## Upstream facts that affect compatibility

- [Kueue v0.20.0 release notes](https://github.com/kubernetes-sigs/kueue/releases/tag/v0.20.0) state that `kueue.x-k8s.io/v1beta1` is no longer served. Clients and manifests must use `v1beta2`; existing objects in an upgraded cluster need the upstream storage migration before upgrading.
- The Go provider, capability discovery, and the retained Python manifest builder now target `kueue.x-k8s.io/v1beta2`. This API-client migration does **not** itself certify Kueue v0.20.3 or an in-place cluster upgrade.
- [Kueue v0.20.3](https://github.com/kubernetes-sigs/kueue/releases/tag/v0.20.3), released 2026-10-22, fixes a concurrent-admission case where a parent Workload could remain admitted on a removed ResourceFlavor. It also fixes a MultiKueue batch/Job redispatch eviction loop seen with Kubernetes 1.35+. The latter is **not** proof that this platform uses or should enable MultiKueue.
- [Kueue v0.20 installation guidance](https://kueue.sigs.k8s.io/v0.20/docs/installation/) recommends Kubernetes 1.34 or newer. [Kubernetes release records](https://kubernetes.io/releases/) list 1.35, 1.36, and 1.37 as supported minor lines as of this review.
- Before any v0.20.x deployment, audit the actual Kueue configuration against the v0.20.0 upgrade notes, especially Fair Sharing feature-gate consistency and `metrics.localQueueMetrics.localQueueSelector`. The current compatibility experiment must not silently enable optional Kueue features.

## Qualification matrix

| ID | Kubernetes image | Kueue | Status | Purpose / exit condition |
| --- | --- | --- | --- | --- |
| C0 | `kindest/node:v1.34.0` | `v0.19.6` | **Frozen baseline / reverified** | Golden Path [run 40063557591](https://github.com/avaargsh/gpu-compute-platform/actions/runs/40063557591) on SHA `65bf8236`; artifact 12547574424. |
| C1 | `kindest/node:v1.34.0` | `v0.20.3` | **Fresh-cluster PASS**, not live upgrade | Golden Path [run 40060012042](https://github.com/avaargsh/gpu-compute-platform/actions/runs/40060012042) on source SHA `d1d283d8`; artifact 12547480915. No storage or rolling-upgrade claim. |
| C2 | `kindest/node:v1.35.8@sha256:07b2536e30b803ed61d1677a79df6115f798ce64c80f9e22f6ed45afd09323c0` | `v0.20.3` | **Fresh-cluster PASS**, not live upgrade | Golden Path [run 40063626936](https://github.com/avaargsh/gpu-compute-platform/actions/runs/40063626936) on SHA `65bf8236` using kind v0.33.0; actual Kubernetes v1.35.8 and Kueue v0.20.3 observed; artifact 12548451722. |
| C3 | Latest published kind image in the Kubernetes 1.37 minor line (record exact tag and observed server version) | `v0.20.3` | Optional follow-up | Qualify the newest supported minor only if the product intends to claim it. Do not infer a kind image tag from the Kubernetes patch number. |

A candidate row becomes supported only when the exact tested commit, actual Kubernetes server version, Kueue image/version, command output, and all gate outcomes are recorded. An unavailable image, skipped test, or passing unit test must not be recorded as real-cluster acceptance.

## Minimum regression gates

### 1. API and discovery contract

```bash
go test ./internal/provider/kueue -run 'TestKueueObjectsAndGVRsUseV1beta2API|TestKubeClientPoolAndWorkloadGoldenPath' -count=1
go test ./internal/cluster -run 'TestDiscoveryUsesV1beta2KueueAPI|TestProductionRuntimeRegistersOnlyAcceptedKueueProvider' -count=1
```

Expected: all Kueue object manifests and dynamic GVRs use `kueue.x-k8s.io/v1beta2`; discovery checks the v1beta2 ClusterQueue endpoint; the production runtime still registers only `kueue`.

### 2. Existing lifecycle contracts

```bash
make acceptance-contract
make test
```

At minimum, retain coverage for:

- missing and unregistered provider identities failing closed across Pool/Workload reconcile and delete;
- Kueue Workload lost-ACK replay and create-race adoption without duplicate Job creation;
- different-generation conflicts rejected;
- lease takeover fencing stale observation/finalization;
- delete/finalize ordering, tombstone/generation safety, and shared ResourceFlavor preservation.

### 3. Real kind/Kueue Golden Path for each candidate row

The script accepts `KIND_NODE_IMAGE` and `KUEUE_VERSION`. Use a **fresh `CLUSTER_ID` for each row**; the script rejects a pre-existing cluster when `KIND_NODE_IMAGE` is explicitly set, because that image cannot replace an existing cluster's Kubernetes version.

Example for C1:

```bash
CLUSTER_ID=kueue-0203-k134 \
KIND_NODE_IMAGE=kindest/node:v1.34.0 \
KUEUE_VERSION=v0.20.3 \
make e2e-golden
```

For C2, use a published digest-pinned `kindest/node:v1.35.x@sha256:<digest>` image with a distinct cluster name, for example `CLUSTER_ID=kueue-0203-k135`. The C2 acceptance uses kind v0.33.0, whereas C1 and the frozen baseline keep kind v0.30.0. Record `kubectl version` output rather than assuming the image's tag is the actual patch version. Keep the real Golden Path manual/opt-in; do not add a costly Kubernetes-version matrix to every ordinary code push.

The real Golden Path must still prove capability registration reports exactly `["kueue"]`, a Workload reaches the expected Kueue admission state, the Job/Pod observation is independently read, invalid accelerator intent fails closed, provider objects are gone before Desired finalization, and the shared ResourceFlavor is not incorrectly deleted.


## Read-only storage migration preflight (mandatory before in-place upgrade review)

The fresh-cluster test runs above do not inspect existing etcd objects. Kueue v0.20 stops serving
`v1beta1`; the [official v0.20.0 release notes](https://github.com/kubernetes-sigs/kueue/releases/tag/v0.20.0)
require migration of existing objects **before** upgrading to the new release.

Run an explicit-context, read-only inventory from an operator workstation:

```bash
KUBECTL_CONTEXT=my-reviewed-cluster make kueue-storage-preflight
# For a saved CRD List fixture (offline; never connects to Kubernetes):
python3 scripts/e2e/kueue_storage_preflight.py --snapshot crds-before.json --report-file preflight.json
```

The preflight uses only `kubectl --context CONTEXT get customresourcedefinitions -o json`;
it performs **no writes**, does not install/upgrade Kueue, and never invokes the upstream migration script.
It checks all discovered `*.kueue.x-k8s.io` CRDs, including optional Kueue kinds; it requires
core ClusterQueue, LocalQueue, ResourceFlavor and Workload CRDs. Each must serve `v1beta2`,
use it as the sole `spec.versions[*].storage` version, and report exactly `["v1beta2"]`
under `status.storedVersions`. Missing or unreadable data, missing RBAC, malformed JSON,
or remaining `v1beta1` storage markers return `BLOCKED` with exit code 2 and JSON evidence.

A `PASS` means only that the queried CRD **version metadata** meets this conservative
precondition at the time of the read. It does **not** establish that all etcd objects were
actually rewritten, that a configuration/feature-gate change is valid, that Kueue controllers
can be rolled back after changing stored objects, or that an in-place upgrade is safe.

For `BLOCKED`, stop. Review the upstream
[migration script](https://github.com/kubernetes-sigs/kueue/blob/main/hack/migrate-to-v1beta2.sh)
at a pinned upstream commit. Its annotation/rewrite operations are privileged mutations;
run it only via a separately reviewed rollout plan with etcd backup, restoration authority,
migration evidence, feature-policy audit and staged controller compatibility checks.
Do not modify `status.storedVersions` simply to make the preflight green, or interpret
client-side/server-side dry run as proof that live objects have been rewritten.

## Isolated in-place upgrade rehearsal (experimental, opt-in)

The repository contains `scripts/e2e/kueue_inplace_upgrade_kind.sh`, a disposable
kind-only 0.19.6 → 0.20.3 controller/CRD upgrade **experiment**. It refuses a
pre-existing cluster, restricts cluster names to `kueue-upgrade-*`, requires
`ALLOW_EPHEMERAL_KUEUE_UPGRADE=YES`, installs an isolated kubeconfig, verifies
the old API exists by creating v1beta1 fixtures, and retrieves the official
migration script from Kueue commit `f0e95cea51b5591e0177fbe0fa06a299f36db548`.
It verifies that exact upstream Git blob `ede30226c5521585aa9aefc261ffb69af88afd38`
before executing it.

It records original CRD and Kueue object identities, migrates with the old API
still served, installs v0.20.3 into **the same** cluster, runs the storage
preflight, compares UIDs and migration annotations, admits a new Job, and runs
the product's complete Go Golden Path against the upgraded kind cluster.
All phases must succeed to report `UPGRADE REHEARSAL PASS`. Unexpected resource
versions, identity changes, or admission failures stop the experiment.

Example invocation from a disposable development environment with kind,
Docker, kubectl, Go, Helm, Python and jq installed:

```bash
ALLOW_EPHEMERAL_KUEUE_UPGRADE=YES \
  CLUSTER_ID=kueue-upgrade-sample01 \
  KIND_NODE_IMAGE=kindest/node:v1.34.0 \
  EVIDENCE_DIR=/tmp/kueue-upgrade-sample01-evidence \
  bash scripts/e2e/kueue_inplace_upgrade_kind.sh
```

**Never invoke this on a production cluster.** The script deletes only the
new disposable kind cluster it created, and never attempts to change the
operator's current kubeconfig. Evidence paths must be new. Its outcome is
bounded to the tested cohort, API-server and controller generation; it does
not validate production etcd backup/restore, partial migration crashes,
a mixed-version rolling upgrade, or a rollback after irreversible writes.

## Live-cluster upgrade boundary

The matrix above creates fresh kind clusters. It does **not** validate an in-place upgrade. Before upgrading a cluster with existing Kueue objects to a version that no longer serves `v1beta1`:

1. review every crossed Kueue minor release's `.0` upgrade notes and the target minor's patch notes;
2. run the upstream Kueue storage migration required by the target release;
3. audit manifests, clients, feature gates, and metrics selector configuration;
4. capture pre/post-upgrade Workload admission, Job/Pod lifecycle, quota, and deletion evidence;
5. retain a rollback plan compatible with the target API/storage state.

## Non-goals

This compatibility work does not register Volcano/KAI, enable MultiKueue, add a second admission path, enable DRA execution, or promote an alpha scheduling feature. The Stage B provider SPI remains an execution boundary; scheduler discovery is inventory, not permission to dispatch.


## Running the compatibility acceptance

The compatibility run is intentionally opt-in so ordinary pushes do not consume kind-cluster CI minutes.

- **C1:** use the existing `kindest/node:v1.34.0` image with Kueue `v0.20.3`.
- **C2:** when workflow dispatch is available on the default branch, use row `C2` and an exact published `kindest/node:v1.35.x@sha256:<digest>` image; do not use an unpinned tag. The isolated evidence run used kind v0.33.0.
- To run C1 directly from this PR without dispatching the full Go/Portal suite, add the `run-kueue-compatibility` label. This label selects only the compatibility job; it does not trigger Portal browser acceptance or the Management ACK-drop experiment.
- The compatibility job checks the v1beta2 API contract and lifecycle/PostgreSQL acceptance, runs the real Golden Path, verifies the actual Kubernetes server minor and Kueue image tag, and uploads a compatibility evidence artifact. Record the artifact together with the checked-out 40-character SHA before changing a row's status.
- Do not merge this PR or mark C1/C2 supported solely because the ordinary Go CI passes. A failed, unavailable, or skipped real-cluster check leaves the candidate row unaccepted.
