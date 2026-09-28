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
  [[ -z "${CONTROL_PID:-}" ]] || { echo "--- control-plane ---" >&2; cat /tmp/go-control-plane.log >&2 || true; }
  [[ -z "${AGENT_PID:-}" ]] || { echo "--- cluster-agent ---" >&2; cat /tmp/go-cluster-agent.log >&2 || true; }
  return 1
}

kind get clusters | grep -qx "$CLUSTER_ID" || kind create cluster --name "$CLUSTER_ID" --image kindest/node:v1.34.0 --wait 120s
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"
kubectl wait --for=condition=Available deployment/kueue-controller-manager -n kueue-system --timeout="${TIMEOUT_SECONDS}s"

node="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl label node "$node" ai.compute/accelerator-class=h100-80g --overwrite
bash scripts/e2e/install-fake-gpu.sh
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

go run ./cmd/control-plane >/tmp/go-control-plane.log 2>&1 &
CONTROL_PID=$!
wait_http

CLUSTER_ID="$CLUSTER_ID" CONTROL_PLANE_URL="$BASE_URL" go run ./cmd/cluster-agent >/tmp/go-cluster-agent.log 2>&1 &
AGENT_PID=$!

put "/api/v1/projects/$PROJECT_ID/binding" "{\"metadata\":{\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"clusterId\":\"$CLUSTER_ID\",\"namespace\":\"$NAMESPACE\"}"
put "/api/v1/compute-pools/$POOL_ID/binding" "{\"metadata\":{\"generation\":1},\"poolId\":\"$POOL_ID\",\"clusterId\":\"$CLUSTER_ID\",\"provider\":\"kueue\"}"
put "/api/v1/compute-pools/$POOL_ID" "{\"metadata\":{\"id\":\"$POOL_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"spec\":{\"accelerators\":[{\"class\":\"h100-80g\",\"quota\":4}],\"scheduling\":{\"mode\":\"default\"}}}"
put "/api/v1/workloads/$WORKLOAD_ID" "{\"metadata\":{\"id\":\"$WORKLOAD_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"echo go-kind-kueue-golden && sleep 5\"],\"accelerator\":{\"class\":\"h100-80g\",\"quota\":1}}}"

result="$(wait_workload)"
condition_true "$result" "Admitted"
requested="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].resources.requests.nvidia\.com/gpu}')"
[[ "$requested" == "1" ]] || { echo "expected nvidia.com/gpu request=1, got $requested" >&2; exit 1; }

echo "$result"
echo "GO GOLDEN PASS: Control Plane -> Agent -> kind -> Kueue -> Job/Pod -> Observation -> Product GET"
