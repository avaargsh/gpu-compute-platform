#!/usr/bin/env bash
# Only for a fresh disposable kind cluster. Never run on production kubeconfigs.
set -euo pipefail

[[ "$ALLOW_EPHEMERAL_KUEUE_UPGRADE" == YES ]] || { echo "BLOCKED: explicit opt-in required" >&2; exit 2; }
[[ "$CLUSTER_ID" =~ ^kueue-upgrade-[a-z0-9-]+$ ]] || { echo "BLOCKED: disposable cluster name required" >&2; exit 2; }
[[ "$KIND_NODE_IMAGE" == "kindest/node:v1.34.0" ]] || { echo "BLOCKED: frozen node image only" >&2; exit 2; }
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

# Run official upstream migration BEFORE v1beta1 is removed from discovery.
bash "$tmp/migrate.sh" --kubectl-get-api-version=v1beta1 > "$EVIDENCE_DIR/migration.log" 2>&1
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
