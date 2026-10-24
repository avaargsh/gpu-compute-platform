# Kueue / Kubernetes Compatibility Matrix

**Review date:** 2026-10-24  
**Scope:** Stage B provider SPI and the single production Kueue execution path.  
**Status:** Candidate rows below are plans, not verified support claims.

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
| C0 | `kindest/node:v1.34.0` | `v0.19.6` | **Frozen baseline** | Existing supported row. Preserve as the rollback/comparison reference. |
| C1 | `kindest/node:v1.34.0` | `v0.20.3` | Candidate; not yet accepted | Isolate the Kueue API/version upgrade on the already-qualified Kubernetes minor. Run all code-level contracts and the real Golden Path. |
| C2 | Published kind image for Kubernetes 1.35.x (record exact tag and observed server version) | `v0.20.3` | Candidate; not yet accepted | Verify the Kueue-only Workload admission, Job/Pod observation, and cleanup path on the Kubernetes minor called out in the upstream MultiKueue fix. Do not add MultiKueue. |
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

For C2, first confirm the selected `kindest/node` image is published, then use its exact tag and a distinct cluster name, for example `CLUSTER_ID=kueue-0203-k135`. Record `kubectl version` output rather than assuming the image's tag is the actual patch version. Keep the real Golden Path manual/opt-in; do not add a costly Kubernetes-version matrix to every ordinary code push.

The real Golden Path must still prove capability registration reports exactly `["kueue"]`, a Workload reaches the expected Kueue admission state, the Job/Pod observation is independently read, invalid accelerator intent fails closed, provider objects are gone before Desired finalization, and the shared ResourceFlavor is not incorrectly deleted.

## Live-cluster upgrade boundary

The matrix above creates fresh kind clusters. It does **not** validate an in-place upgrade. Before upgrading a cluster with existing Kueue objects to a version that no longer serves `v1beta1`:

1. review every crossed Kueue minor release's `.0` upgrade notes and the target minor's patch notes;
2. run the upstream Kueue storage migration required by the target release;
3. audit manifests, clients, feature gates, and metrics selector configuration;
4. capture pre/post-upgrade Workload admission, Job/Pod lifecycle, quota, and deletion evidence;
5. retain a rollback plan compatible with the target API/storage state.

## Non-goals

This compatibility work does not register Volcano/KAI, enable MultiKueue, add a second admission path, enable DRA execution, or promote an alpha scheduling feature. The Stage B provider SPI remains an execution boundary; scheduler discovery is inventory, not permission to dispatch.
