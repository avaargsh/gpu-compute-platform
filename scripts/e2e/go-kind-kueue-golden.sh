#!/usr/bin/env bash
set -euo pipefail

: "${CLUSTER_ID:=kind-golden}"
: "${BASE_URL:=http://127.0.0.1:8080}"
: "${KUEUE_VERSION:=v0.19.6}"
: "${TIMEOUT_SECONDS:=240}"
: "${NAMESPACE:=golden-go}"
: "${PROJECT_ID:=project-golden}"
: "${POOL_ID:=pool-h100}"
: "${WORKLOAD_ID:=train-golden}"
: "${INVALID_WORKLOAD_ID:=train-invalid}"
: "${ACCELERATOR_RESOURCE:=nvidia.com/gpu}"
: "${ACCELERATOR_FLAVOR:=h100-80g}"

cleanup() {
  [[ -z "${AGENT_PID:-}" ]] || kill "$AGENT_PID" 2>/dev/null || true
  [[ -z "${CONTROL_PID:-}" ]] || kill "$CONTROL_PID" 2>/dev/null || true
}
trap cleanup EXIT

for cmd in curl kubectl kind helm go python; do
  command -v "$cmd" >/dev/null || { echo "missing required command: $cmd" >&2; exit 2; }
done

wait_http() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS))
  until curl -fsS "$BASE_URL/healthz" >/dev/null; do
    (( SECONDS < deadline )) || { echo "control plane did not become ready" >&2; return 1; }
    sleep 1
  done
}

put() {
  curl -fsS -X PUT "$BASE_URL$1" -H 'Content-Type: application/json' -d "$2" >/dev/null
}

condition_true() {
  local payload="$1" type="$2"
  PAYLOAD="$payload" TYPE="$type" python - <<'PY'
import json, os
data=json.loads(os.environ["PAYLOAD"])
conditions=(data.get("status") or {}).get("conditions") or []
raise SystemExit(0 if any(c.get("type")==os.environ["TYPE"] and c.get("status")=="True" for c in conditions) else 1)
PY
}

wait_workload() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS)) payload
  while (( SECONDS < deadline )); do
    payload="$(curl -fsS "$BASE_URL/api/v1/workloads/$WORKLOAD_ID")"
    if PAYLOAD="$payload" python - <<'PY'
import json, os
data=json.loads(os.environ["PAYLOAD"])
status=data.get("status") or {}
conditions=status.get("conditions") or []
truth={c.get("type") for c in conditions if c.get("status")=="True"}
ok=(status.get("syncState")=="Synced"
    and "QuotaReserved" in truth
    and "Admitted" in truth
    and ("Ready" in truth or "Succeeded" in truth))
raise SystemExit(0 if ok else 1)
PY
    then
      printf '%s\n' "$payload"
      return 0
    fi
    sleep 2
  done
  echo "timeout waiting for Go golden workload" >&2
  kubectl get resourceflavors,clusterqueues -o wide >&2 || true
  kubectl get localqueues,workloads,jobs,pods -n "$NAMESPACE" -o wide >&2 || true
  pod="$(kubectl get pods -n "$NAMESPACE" -l "ai.compute/workload=job-$WORKLOAD_ID" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -z "$pod" ]] || kubectl describe pod "$pod" -n "$NAMESPACE" >&2 || true
  kubectl describe node "$node" >&2 || true
  [[ -z "${CONTROL_PID:-}" ]] || { echo "--- control-plane ---" >&2; cat /tmp/go-control-plane.log >&2 || true; }
  [[ -z "${AGENT_PID:-}" ]] || { echo "--- cluster-agent ---" >&2; cat /tmp/go-cluster-agent.log >&2 || true; }
  return 1
}

kind get clusters | grep -qx "$CLUSTER_ID" || kind create cluster --name "$CLUSTER_ID" --image kindest/node:v1.34.0 --wait 120s
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"
kubectl wait --for=condition=Available deployment/kueue-controller-manager -n kueue-system --timeout="${TIMEOUT_SECONDS}s"

node="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl label node "$node" nvidia.com/gpu.product=NVIDIA-H100-80GB-HBM3 --overwrite
bash scripts/e2e/install-fake-gpu.sh
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

go run ./cmd/control-plane >/tmp/go-control-plane.log 2>&1 &
CONTROL_PID=$!
wait_http

CLUSTER_ID="$CLUSTER_ID" CONTROL_PLANE_URL="$BASE_URL" go run ./cmd/cluster-agent >/tmp/go-cluster-agent.log 2>&1 &
AGENT_PID=$!

put "/api/v1/projects/$PROJECT_ID/binding" "{\"metadata\":{\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"clusterId\":\"$CLUSTER_ID\",\"namespace\":\"$NAMESPACE\"}"
put "/api/v1/compute-pools/$POOL_ID/binding" "{\"metadata\":{\"generation\":1},\"poolId\":\"$POOL_ID\",\"clusterId\":\"$CLUSTER_ID\",\"provider\":\"kueue\"}"
put "/api/v1/compute-pools/$POOL_ID" "{\"metadata\":{\"id\":\"$POOL_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"spec\":{\"accelerators\":[{\"class\":\"h100-80g\",\"quota\":4}],\"acceleratorBindings\":[{\"class\":\"h100-80g\",\"resourceName\":\"$ACCELERATOR_RESOURCE\",\"flavor\":\"$ACCELERATOR_FLAVOR\",\"nodeLabels\":{\"nvidia.com/gpu.product\":\"NVIDIA-H100-80GB-HBM3\"}}],\"scheduling\":{\"mode\":\"default\"}}}"
put "/api/v1/workloads/$WORKLOAD_ID" "{\"metadata\":{\"id\":\"$WORKLOAD_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"echo go-kind-kueue-golden && sleep 5\"],\"accelerator\":{\"class\":\"h100-80g\",\"quota\":1}}}"

result="$(wait_workload)"
condition_true "$result" "Admitted"
requested="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].resources.requests.nvidia\.com/gpu}')"
[[ "$requested" == "1" ]] || { echo "expected $ACCELERATOR_RESOURCE request=1, got $requested" >&2; exit 1; }
flavor_label="$(kubectl get resourceflavor "$ACCELERATOR_FLAVOR" -o jsonpath='{.spec.nodeLabels.nvidia\.com/gpu\.product}')"
[[ "$flavor_label" == "NVIDIA-H100-80GB-HBM3" ]] || { echo "unexpected H100 flavor node label: $flavor_label" >&2; exit 1; }

# Replaying desired state must not drift the resolved Kubernetes projection.
before="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o json)"
put "/api/v1/workloads/$WORKLOAD_ID" "{\"metadata\":{\"id\":\"$WORKLOAD_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"echo go-kind-kueue-golden && sleep 5\"],\"accelerator\":{\"class\":\"h100-80g\",\"quota\":1}}}"
sleep 3
after="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o json)"
BEFORE="$before" AFTER="$after" python - <<'PY'
import json, os
before=json.loads(os.environ["BEFORE"])
after=json.loads(os.environ["AFTER"])
def projection(obj):
    c=obj["spec"]["template"]["spec"]["containers"][0]
    return {
        "requests": c["resources"]["requests"],
        "limits": c["resources"]["limits"],
        "annotations": obj["metadata"].get("annotations", {}),
        "labels": obj["metadata"].get("labels", {}),
    }
assert projection(before) == projection(after), (projection(before), projection(after))
PY

# An unbound portable class must fail closed before any Kubernetes Job is created.
put "/api/v1/workloads/$INVALID_WORKLOAD_ID" "{\"metadata\":{\"id\":\"$INVALID_WORKLOAD_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"exit 0\"],\"accelerator\":{\"class\":\"a100-invalid\",\"quota\":1}}}"
sleep 3
invalid="$(curl -fsS "$BASE_URL/api/v1/workloads/$INVALID_WORKLOAD_ID")"
PAYLOAD="$invalid" python - <<'PY'
import json, os
data=json.loads(os.environ["PAYLOAD"])
conditions=(data.get("status") or {}).get("conditions") or []
failed=[c for c in conditions if c.get("type")=="Ready" and c.get("status")=="False" and c.get("reason")=="ReconcileFailed"]
assert failed, conditions
assert "accelerator binding not found: a100-invalid" in (failed[0].get("message") or ""), failed[0]
PY
if kubectl get job "job-$INVALID_WORKLOAD_ID" -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "fail-closed violated: invalid accelerator workload created a Job" >&2
  exit 1
fi

echo "$result"
echo "GO GOLDEN PASS: Control Plane -> Agent -> kind -> Kueue -> Job/Pod -> Observation -> Product GET"
