#!/usr/bin/env bash
# Only for a fresh disposable kind cluster. Never run on production kubeconfigs.
set -euo pipefail

[[ "$ALLOW_EPHEMERAL_KUEUE_UPGRADE" == YES ]] || { echo "BLOCKED: explicit opt-in required" >&2; exit 2; }
[[ "$CLUSTER_ID" =~ ^kueue-upgrade-[a-z0-9-]+$ ]] || { echo "BLOCKED: disposable cluster name required" >&2; exit 2; }
[[ "$KIND_NODE_IMAGE" == "kindest/node:v1.34.0" ]] || { echo "BLOCKED: frozen node image only" >&2; exit 2; }
[[ "${LEGACY_ETCD_FIXTURE:-NO}" == "NO" || "${LEGACY_ETCD_FIXTURE:-NO}" == "YES" ]] || {
  echo "BLOCKED: LEGACY_ETCD_FIXTURE must be YES or NO" >&2; exit 2;
}
[[ ! -e "$EVIDENCE_DIR" ]] || { echo "BLOCKED: evidence directory must be new" >&2; exit 2; }
for cmd in kind kubectl curl git python3 jq sha256sum go helm; do
  command -v "$cmd" >/dev/null || { echo "BLOCKED: missing $cmd" >&2; exit 2; }
done
if kind get clusters | grep -Fxq "$CLUSTER_ID"; then
  echo "BLOCKED: never adopt an existing cluster" >&2; exit 2
fi
mkdir -p "$EVIDENCE_DIR"
EVIDENCE_DIR="$(cd "$EVIDENCE_DIR" && pwd)"
tmp="$(mktemp -d)"
created=0
cleanup() {
  status=$?
  if [[ "$created" == 1 ]]; then kind delete cluster --name "$CLUSTER_ID" >/dev/null || true; fi
  rm -rf "$tmp"
  exit "$status"
}
trap cleanup EXIT

# An isolated KUBECONFIG is stronger than merely trusting current-context.
export KUBECONFIG="$tmp/kubeconfig"
kind create cluster --name "$CLUSTER_ID" --image "$KIND_NODE_IMAGE" --wait 120s
created=1
[[ "$(kubectl config current-context)" == "kind-$CLUSTER_ID" ]] || {
  echo "BLOCKED: kind context mismatch" >&2; exit 2;
}
kubectl version -o json > "$EVIDENCE_DIR/kubernetes.json"
python3 - "$EVIDENCE_DIR/kubernetes.json" <<'PY'
import json,sys
v=json.load(open(sys.argv[1]))["serverVersion"]["gitVersion"]
assert v.startswith("v1.34."), f"unexpected Kubernetes server version: {v}"
PY
{
  echo "source_sha=$(git rev-parse HEAD)"
  echo "node_image=$KIND_NODE_IMAGE"
  echo "from_kueue=v0.19.6"
  echo "to_kueue=v0.20.3"
  echo "legacy_etcd_fixture=${LEGACY_ETCD_FIXTURE:-NO}"
  echo "upstream_migration_commit=f0e95cea51b5591e0177fbe0fa06a299f36db548"
  echo "upstream_migration_blob=ede30226c5521585aa9aefc261ffb69af88afd38"
} > "$EVIDENCE_DIR/generation.txt"
curl -fsSL --retry 3 --retry-all-errors --connect-timeout 15 --max-time 90 \
  https://raw.githubusercontent.com/kubernetes-sigs/kueue/f0e95cea51b5591e0177fbe0fa06a299f36db548/hack/migrate-to-v1beta2.sh \
  -o "$tmp/migrate.sh"
[[ "$(git hash-object "$tmp/migrate.sh")" == "ede30226c5521585aa9aefc261ffb69af88afd38" ]] || {
  echo "BLOCKED: migration blob differs from the audited upstream v0.19.6 version" >&2; exit 2;
}
sha256sum "$tmp/migrate.sh" > "$EVIDENCE_DIR/upstream-migration.sha256"

kubectl apply --server-side -f \
  https://github.com/kubernetes-sigs/kueue/releases/download/v0.19.6/manifests.yaml \
  > "$EVIDENCE_DIR/install-old.log"
kubectl rollout status -n kueue-system deployment/kueue-controller-manager --timeout=240s
kubectl create namespace kueue-upgrade-fixture

# Mutation helpers are intentionally scoped to the brand-new disposable kind
# cluster via the private KUBECONFIG; they must not be exported for operators.
switch_resourceflavor_storage() {
  local target="$1"
  kubectl get crd resourceflavors.kueue.x-k8s.io -o json > "$tmp/resourceflavor-crd.json"
  python3 - "$tmp/resourceflavor-crd.json" "$target" > "$tmp/storage-patch.json" <<'PY'
import json, sys
crd=json.load(open(sys.argv[1]))
target=sys.argv[2]
versions=crd["spec"]["versions"]
names=[x["name"] for x in versions]
assert "v1beta1" in names and "v1beta2" in names, names
assert all(x.get("served") is True for x in versions if x["name"] in ("v1beta1","v1beta2"))
ops=[{"op":"replace","path":f"/spec/versions/{i}/storage",
      "value":v["name"]==target} for i,v in enumerate(versions)]
assert sum(x["value"] is True for x in ops)==1
print(json.dumps(ops))
PY
  kubectl patch crd resourceflavors.kueue.x-k8s.io --type=json \
    -p "$(cat "$tmp/storage-patch.json")"
  kubectl wait --for=condition=Established crd/resourceflavors.kueue.x-k8s.io --timeout=60s
  kubectl get crd resourceflavors.kueue.x-k8s.io -o json |
    python3 -c 'import json,sys; target=sys.argv[1]; data=json.load(sys.stdin); assert [v["name"] for v in data["spec"]["versions"] if v["storage"]]==[target]' "$target"
}

probe_disposable_etcd() {
  local expected="$1"
  # This is the *only* direct etcd read. No etcd write flags are supported.
  local pod
  pod="$(kubectl get pods -n kube-system -l component=etcd -o jsonpath='{.items[0].metadata.name}')"
  [[ "$pod" == "etcd-$CLUSTER_ID-control-plane" ]] || {
    echo "BLOCKED: unexpected etcd pod in disposable kind" >&2; exit 2;
  }
  # Generic CRD storage prefixes are an API-server implementation detail.
  # Enumerate only keys (no object values), then require exactly one fixture
  # key. Do not assume an etcd key prefix from its public API resource name.
  local key
  key="$(kubectl -n kube-system exec "$pod" -- etcdctl \
    --endpoints=https://127.0.0.1:2379 \
    --cacert=/etc/kubernetes/pki/etcd/ca.crt \
    --cert=/etc/kubernetes/pki/etcd/healthcheck-client.crt \
    --key=/etc/kubernetes/pki/etcd/healthcheck-client.key \
    get /registry/ --prefix --keys-only --write-out=json |
    python3 scripts/e2e/kueue_etcd_fixture_probe.py --discover-key)"
  kubectl -n kube-system exec "$pod" -- etcdctl \
    --endpoints=https://127.0.0.1:2379 \
    --cacert=/etc/kubernetes/pki/etcd/ca.crt \
    --cert=/etc/kubernetes/pki/etcd/healthcheck-client.crt \
    --key=/etc/kubernetes/pki/etcd/healthcheck-client.key \
    get "$key" --write-out=json |
    python3 scripts/e2e/kueue_etcd_fixture_probe.py --key "$key" --expected-version "$expected" \
      --evidence-file "$EVIDENCE_DIR/etcd-$expected.json"
}

if [[ "${LEGACY_ETCD_FIXTURE:-NO}" == YES ]]; then
  switch_resourceflavor_storage v1beta1
fi
cat > "$tmp/fixture.yaml" <<'YAML'
apiVersion: kueue.x-k8s.io/v1beta1
kind: ResourceFlavor
metadata:
  name: rehearsal-flavor
spec: {}
---
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: rehearsal-clusterqueue
spec:
  namespaceSelector: {}
  resourceGroups:
    - coveredResources: ["cpu", "memory"]
      flavors:
        - name: rehearsal-flavor
          resources:
            - name: cpu
              nominalQuota: "2"
            - name: memory
              nominalQuota: "2Gi"
---
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  name: rehearsal-queue
  namespace: kueue-upgrade-fixture
spec:
  clusterQueue: rehearsal-clusterqueue
---
apiVersion: batch/v1
kind: Job
metadata:
  name: retained-job
  namespace: kueue-upgrade-fixture
  labels:
    kueue.x-k8s.io/queue-name: rehearsal-queue
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: busybox
          image: busybox:1.36
          command: ["sh", "-c", "sleep 240"]
          resources:
            requests: {cpu: "100m", memory: "16Mi"}
            limits: {cpu: "500m", memory: "64Mi"}
YAML
for attempt in $(seq 1 30); do
  if kubectl apply -f "$tmp/fixture.yaml" > "$EVIDENCE_DIR/fixture-create.log" 2>&1; then break; fi
  (( attempt < 30 )) || { cat "$EVIDENCE_DIR/fixture-create.log" >&2; exit 1; }
  sleep 2
done
for attempt in $(seq 1 90); do
  [[ -n "$(kubectl get workloads.kueue.x-k8s.io -n kueue-upgrade-fixture -o jsonpath='{.items[0].metadata.uid}' 2>/dev/null || true)" ]] && break
  (( attempt < 90 )) || { echo "BLOCKED: old-version Workload not created" >&2; exit 1; }
  sleep 2
done
for key in resourceflavor clusterqueue localqueue; do
  case "$key" in
    resourceflavor) kubectl get resourceflavor.kueue.x-k8s.io rehearsal-flavor -o json > "$EVIDENCE_DIR/$key-before.json";;
    clusterqueue) kubectl get clusterqueue.kueue.x-k8s.io rehearsal-clusterqueue -o json > "$EVIDENCE_DIR/$key-before.json";;
    localqueue) kubectl get localqueue.kueue.x-k8s.io rehearsal-queue -n kueue-upgrade-fixture -o json > "$EVIDENCE_DIR/$key-before.json";;
  esac
done
kubectl get job retained-job -n kueue-upgrade-fixture -o json > "$EVIDENCE_DIR/job-before.json"
kubectl get workloads.kueue.x-k8s.io -n kueue-upgrade-fixture -o json > "$EVIDENCE_DIR/workloads-before.json"
kubectl get crds -o json > "$EVIDENCE_DIR/crds-before.json"
if [[ "${LEGACY_ETCD_FIXTURE:-NO}" == YES ]]; then
  # Fail if the CRD status does not confirm v1beta1 was actually a storage version.
  python3 - "$EVIDENCE_DIR/crds-before.json" <<'PY'
import json,sys
data=json.load(open(sys.argv[1]))
crd=next(x for x in data["items"] if x["metadata"]["name"]=="resourceflavors.kueue.x-k8s.io")
assert "v1beta1" in crd["status"]["storedVersions"], "old storage version was never established"
PY
  probe_disposable_etcd v1beta1
  # Newly annotated objects must be written in v1beta2 for the migration
  # to be effective; changing a CRD's storage version does NOT rewrite data.
  switch_resourceflavor_storage v1beta2
fi

# Run official upstream migration BEFORE v1beta1 is removed from discovery.
bash "$tmp/migrate.sh" --kubectl-get-api-version=v1beta1 > "$EVIDENCE_DIR/migration.log" 2>&1
if [[ "${LEGACY_ETCD_FIXTURE:-NO}" == YES ]]; then
  # Verify raw etcd bytes changed BEFORE retiring the legacy stored version.
  probe_disposable_etcd v1beta2
  # Kubernetes only permits dropping storedVersions after all objects have
  # been rewritten. This is safe here because the brand-new kind contains
  # exactly one intentionally seeded ResourceFlavor legacy object.
  kubectl patch crd resourceflavors.kueue.x-k8s.io --subresource=status     --type=merge -p '{"status":{"storedVersions":["v1beta2"]}}'
fi
kubectl get crds -o json > "$EVIDENCE_DIR/crds-after-migration.json"
kubectl apply --server-side -f \
  https://github.com/kubernetes-sigs/kueue/releases/download/v0.20.3/manifests.yaml \
  > "$EVIDENCE_DIR/upgrade.log"
kubectl rollout status -n kueue-system deployment/kueue-controller-manager --timeout=240s
kubectl get deployment kueue-controller-manager -n kueue-system -o json > "$EVIDENCE_DIR/controller-after.json"
image="$(kubectl get deployment kueue-controller-manager -n kueue-system -o jsonpath='{.spec.template.spec.containers[0].image}')"
[[ "$image" == "registry.k8s.io/kueue/kueue:v0.20.3" ]] || { echo "Wrong Kueue image: $image" >&2; exit 1; }
kubectl get crds -o json > "$EVIDENCE_DIR/crds-after.json"
python3 scripts/e2e/kueue_storage_preflight.py --snapshot "$EVIDENCE_DIR/crds-after.json" --report-file "$EVIDENCE_DIR/storage-preflight.json"
# Report whether this rehearsal actually saw any v1beta1 storedVersions markers.
# v1beta1 API input alone is not evidence that etcd stored v1beta1 objects.
python3 scripts/e2e/kueue_upgrade_lineage.py --evidence-dir "$EVIDENCE_DIR"

for key in resourceflavor clusterqueue localqueue; do
  case "$key" in
    resourceflavor) kubectl get resourceflavor.kueue.x-k8s.io rehearsal-flavor -o json > "$EVIDENCE_DIR/$key-after.json";;
    clusterqueue) kubectl get clusterqueue.kueue.x-k8s.io rehearsal-clusterqueue -o json > "$EVIDENCE_DIR/$key-after.json";;
    localqueue) kubectl get localqueue.kueue.x-k8s.io rehearsal-queue -n kueue-upgrade-fixture -o json > "$EVIDENCE_DIR/$key-after.json";;
  esac
done
kubectl get job retained-job -n kueue-upgrade-fixture -o json > "$EVIDENCE_DIR/job-after.json"
kubectl get workloads.kueue.x-k8s.io -n kueue-upgrade-fixture -o json > "$EVIDENCE_DIR/workloads-after.json"
python3 - "$EVIDENCE_DIR" <<'PY'
import json
from pathlib import Path
import sys
p=Path(sys.argv[1])
def load(name):
    return json.loads((p/name).read_text())
for kind in ("resourceflavor","clusterqueue","localqueue","job"):
    a,b=load(kind+"-before.json"),load(kind+"-after.json")
    assert a["metadata"]["uid"]==b["metadata"]["uid"], "UID changed: "+kind
for kind in ("resourceflavor","clusterqueue","localqueue"):
    a=load(kind+"-after.json")
    assert a["metadata"].get("annotations",{}).get("kueue.x-k8s.io/storage-version")=="v1beta2", "migration annotation missing: "+kind
old={o["metadata"]["uid"] for o in load("workloads-before.json")["items"]}
new={o["metadata"]["uid"] for o in load("workloads-after.json")["items"]}
assert old and old<=new, "old Workloads lost or replaced"
print("PASS: persisted object and Workload UIDs preserved")
PY

# Admit a new workload after the same cluster was upgraded.
cat > "$tmp/after.yaml" <<'YAML'
apiVersion: batch/v1
kind: Job
metadata:
  name: post-upgrade-job
  namespace: kueue-upgrade-fixture
  labels:
    kueue.x-k8s.io/queue-name: rehearsal-queue
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: busybox
          image: busybox:1.36
          command: ["sh", "-c", "echo upgraded"]
          resources:
            requests: {cpu: "100m", memory: "16Mi"}
            limits: {cpu: "500m", memory: "64Mi"}
YAML
kubectl apply -f "$tmp/after.yaml"
kubectl wait job/post-upgrade-job -n kueue-upgrade-fixture --for=condition=Complete --timeout=240s
kubectl get jobs,workloads,pods -n kueue-upgrade-fixture -o json > "$EVIDENCE_DIR/fixture-after-new-job.json"

# The product Golden Path MUST run in this same upgraded cluster.
env -u KIND_NODE_IMAGE CLUSTER_ID="$CLUSTER_ID" KUEUE_VERSION=v0.20.3 make e2e-golden \
  > "$EVIDENCE_DIR/product-golden.log" 2>&1
grep -F "GO GOLDEN PASS:" "$EVIDENCE_DIR/product-golden.log"
kubectl get job post-upgrade-job -n kueue-upgrade-fixture >/dev/null
echo "UPGRADE REHEARSAL PASS: official migration, preserved UIDs, new admission, product Golden Path"
