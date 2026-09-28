#!/usr/bin/env bash
set -euo pipefail

: "${FAKE_GPU_NAMESPACE:=gpu-operator}"
: "${FAKE_GPU_RELEASE:=fake-gpu-operator}"
: "${FAKE_GPU_CHART:=oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator}"
: "${FAKE_GPU_VERSION:=0.2.0}"
: "${FAKE_GPU_POOL:=default}"
: "${FAKE_GPU_PRODUCT:=NVIDIA-H100-80GB-HBM3}"
: "${FAKE_GPU_COUNT:=4}"
: "${FAKE_GPU_MEMORY_MIB:=81920}"
: "${TIMEOUT_SECONDS:=180}"

for cmd in kubectl helm; do
  command -v "$cmd" >/dev/null || { echo "missing required command: $cmd" >&2; exit 2; }
done

node="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl label node "$node" "run.ai/simulated-gpu-node-pool=$FAKE_GPU_POOL" --overwrite
kubectl create namespace "$FAKE_GPU_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace "$FAKE_GPU_NAMESPACE" pod-security.kubernetes.io/enforce=privileged --overwrite

helm upgrade --install "$FAKE_GPU_RELEASE" "$FAKE_GPU_CHART" \
  --namespace "$FAKE_GPU_NAMESPACE" \
  --version "$FAKE_GPU_VERSION" \
  --set "topology.nodePools.$FAKE_GPU_POOL.gpuProduct=$FAKE_GPU_PRODUCT" \
  --set "topology.nodePools.$FAKE_GPU_POOL.gpuCount=$FAKE_GPU_COUNT" \
  --set "topology.nodePools.$FAKE_GPU_POOL.gpuMemory=$FAKE_GPU_MEMORY_MIB" \
  --wait --timeout "${TIMEOUT_SECONDS}s"

# The fake GPU operator owns both the extended GPU resource and simulated
# hardware identity. The Golden Path must consume that identity rather than
# racing the operator with an out-of-band nvidia.com/gpu.product label.
deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  allocatable="$(kubectl get node "$node" -o jsonpath='{.status.allocatable.nvidia\.com/gpu}' 2>/dev/null || true)"
  product="$(kubectl get node "$node" -o jsonpath='{.metadata.labels.nvidia\.com/gpu\.product}' 2>/dev/null || true)"
  if [[ "$allocatable" == "$FAKE_GPU_COUNT" && "$product" == "$FAKE_GPU_PRODUCT" ]]; then
    # Verify the operator remains the stable owner across another reconciliation
    # interval instead of accepting a transient label value.
    sleep 3
    product="$(kubectl get node "$node" -o jsonpath='{.metadata.labels.nvidia\.com/gpu\.product}' 2>/dev/null || true)"
    if [[ "$product" == "$FAKE_GPU_PRODUCT" ]]; then
      echo "Fake GPU ready: node=$node nvidia.com/gpu=$allocatable product=$product"
      exit 0
    fi
  fi
  sleep 2
done

kubectl get node "$node" -o yaml >&2
kubectl get pods -n "$FAKE_GPU_NAMESPACE" -o wide >&2 || true
echo "fake GPU did not converge to product=$FAKE_GPU_PRODUCT count=$FAKE_GPU_COUNT" >&2
exit 1
