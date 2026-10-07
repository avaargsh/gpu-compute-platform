#!/usr/bin/env bash
set -euo pipefail

# Stage B falsification only. This script never registers Volcano in the
# production Cluster Agent and is intentionally excluded from v0.1 acceptance.
: "${CLUSTER_ID:=kind-volcano-contract}"
: "${VOLCANO_VERSION:=v1.15.3}"
: "${NAMESPACE:=volcano-contract}"
: "${TIMEOUT_SECONDS:=240}"

for cmd in kubectl kind; do
  command -v "$cmd" >/dev/null || { echo "missing required command: $cmd" >&2; exit 2; }
done

kind get clusters | grep -qx "$CLUSTER_ID" ||
  kind create cluster --name "$CLUSTER_ID" --image kindest/node:v1.34.0 --wait 120s

# Pin the experiment to an explicit upstream release. Do not use /latest/.
kubectl apply -f "https://raw.githubusercontent.com/volcano-sh/volcano/${VOLCANO_VERSION}/installer/volcano-development.yaml"
kubectl wait --for=condition=Available deployment/volcano-controllers -n volcano-system --timeout="${TIMEOUT_SECONDS}s"
kubectl wait --for=condition=Available deployment/volcano-scheduler -n volcano-system --timeout="${TIMEOUT_SECONDS}s"

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

cat <<'YAML' | kubectl apply -f -
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: stage-b-contract
spec:
  weight: 1
YAML

cat <<YAML | kubectl apply -f -
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: stage-b-contract
  namespace: $NAMESPACE
  annotations:
    ai.compute/provider: volcano
    ai.compute/resource-id: stage-b-contract
    ai.compute/generation: "1"
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: stage-b-contract
  tasks:
  - replicas: 1
    name: worker
    template:
      metadata:
        labels:
          ai.compute/workload: stage-b-contract
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: busybox:1.36
          command: ["sh", "-c", "echo volcano-stage-b-contract && sleep 30"]
          resources:
            requests:
              example.com/gpu: "1"
            limits:
              example.com/gpu: "1"
YAML

# kind has no GPU. Advertise a fake whole-GPU-style extended resource only for
# scheduler-contract validation; this is not real-GPU evidence.
node="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl proxy >/tmp/volcano-contract-proxy.log 2>&1 &
proxy_pid=$!
trap 'kill "$proxy_pid" 2>/dev/null || true' EXIT
sleep 1
curl -fsS -X PATCH -H 'Content-Type: application/json-patch+json'   --data '[{"op":"add","path":"/status/capacity/example.com~1gpu","value":"1"},{"op":"add","path":"/status/allocatable/example.com~1gpu","value":"1"}]'   "http://127.0.0.1:8001/api/v1/nodes/$node/status" >/dev/null

deadline=$((SECONDS + TIMEOUT_SECONDS))
podgroup=""
pod=""
while (( SECONDS < deadline )); do
  podgroup="$(kubectl get podgroups.scheduling.volcano.sh -n "$NAMESPACE" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  pod="$(kubectl get pods -n "$NAMESPACE" -l ai.compute/workload=stage-b-contract -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "$podgroup" && -n "$pod" ]] && break
  sleep 2
done
[[ -n "$podgroup" ]] || { echo "Volcano controller did not create PodGroup" >&2; exit 1; }
[[ -n "$pod" ]] || { echo "Volcano controller did not create Pod" >&2; exit 1; }

job_uid="$(kubectl get jobs.batch.volcano.sh stage-b-contract -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')"
pg_owner_uid="$(kubectl get podgroups.scheduling.volcano.sh "$podgroup" -n "$NAMESPACE" -o jsonpath='{.metadata.ownerReferences[0].uid}')"
pod_owner_uid="$(kubectl get pod "$pod" -n "$NAMESPACE" -o jsonpath='{.metadata.ownerReferences[0].uid}')"
[[ -n "$job_uid" ]] || { echo "missing VolcanoJob UID" >&2; exit 1; }
[[ -n "$pg_owner_uid" ]] || { echo "PodGroup has no owner evidence" >&2; exit 1; }
[[ -n "$pod_owner_uid" ]] || { echo "Pod has no owner evidence" >&2; exit 1; }

# Same-generation replay must preserve provider identity.
kubectl apply -f - <<YAML
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: stage-b-contract
  namespace: $NAMESPACE
  annotations:
    ai.compute/provider: volcano
    ai.compute/resource-id: stage-b-contract
    ai.compute/generation: "1"
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: stage-b-contract
  tasks:
  - replicas: 1
    name: worker
    template:
      metadata:
        labels:
          ai.compute/workload: stage-b-contract
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: busybox:1.36
          command: ["sh", "-c", "echo volcano-stage-b-contract && sleep 30"]
          resources:
            requests: {example.com/gpu: "1"}
            limits: {example.com/gpu: "1"}
YAML
after_uid="$(kubectl get jobs.batch.volcano.sh stage-b-contract -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')"
[[ "$job_uid" == "$after_uid" ]] || { echo "same-generation replay replaced VolcanoJob" >&2; exit 1; }

kubectl delete jobs.batch.volcano.sh stage-b-contract -n "$NAMESPACE" --wait=false
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  if ! kubectl get jobs.batch.volcano.sh stage-b-contract -n "$NAMESPACE" >/dev/null 2>&1; then
    live_pg="$(kubectl get podgroups.scheduling.volcano.sh -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d ' ')"
    live_pods="$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d ' ')"
    [[ "$live_pg" == "0" && "$live_pods" == "0" ]] && break
  fi
  sleep 2
done
kubectl get jobs.batch.volcano.sh stage-b-contract -n "$NAMESPACE" >/dev/null 2>&1 && { echo "VolcanoJob cleanup incomplete" >&2; exit 1; }
[[ "$(kubectl get podgroups.scheduling.volcano.sh -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d ' ')" == "0" ]] || { echo "PodGroup survived parent cleanup" >&2; exit 1; }
[[ "$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d ' ')" == "0" ]] || { echo "Pod survived parent cleanup" >&2; exit 1; }

echo "VOLCANO CONTRACT PASS version=$VOLCANO_VERSION job_uid=$job_uid podgroup=$podgroup pod=$pod"
