#!/usr/bin/env bash
set -euo pipefail

: "${FAKE_GPU_NAMESPACE:=gpu-operator}"
: "${FAKE_GPU_RELEASE:=fake-gpu-operator}"
: "${FAKE_GPU_CHART:=oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator}"
: "${FAKE_GPU_COUNT:=8}"
: "${FAKE_GPU_PRODUCT:=NVIDIA-A100-SXM4-80GB}"
: "${TIMEOUT_SECONDS:=180}"

for cmd in kubectl helm; do
  command -v "$cmd" >/dev/null || { echo "missing required command: $cmd" >&2; exit 2; }
done

node="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl label node "$node" run.ai/simulated-gpu-node-pool=default --overwrite
kubectl create namespace "$FAKE_GPU_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace "$FAKE_GPU_NAMESPACE" pod-security.kubernetes.io/enforce=privileged --overwrite

helm upgrade --install "$FAKE_GPU_RELEASE" "$FAKE_GPU_CHART" \
  --namespace "$FAKE_GPU_NAMESPACE" \
  --wait --timeout "${TIMEOUT_SECONDS}s"

# Keep the acceptance contract independent of chart-internal topology value
# names: FGO's default node-pool is selected by the node label above. The
# observable contract we require is nvidia.com/gpu becoming allocatable.

deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  allocatable="$(kubectl get node "$node" -o jsonpath='{.status.allocatable.nvidia\.com/gpu}' 2>/dev/null || true)"
  if [[ -n "$allocatable" && "$allocatable" != "0" ]]; then
    echo "Fake GPU ready: node=$node nvidia.com/gpu=$allocatable product=$FAKE_GPU_PRODUCT"
    exit 0
  fi
  sleep 2
done

kubectl get node "$node" -o yaml >&2
kubectl get pods -n "$FAKE_GPU_NAMESPACE" -o wide >&2 || true
echo "fake GPU did not become allocatable" >&2
exit 1
