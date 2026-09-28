#!/usr/bin/env bash
set -euo pipefail

: "${FAKE_GPU_NAMESPACE:=gpu-operator}"
: "${FAKE_GPU_RELEASE:=fake-gpu-operator}"
: "${FAKE_GPU_CHART:=oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator}"
: "${FAKE_GPU_VERSION:=0.2.0}"
: "${TIMEOUT_SECONDS:=180}"

for cmd in kubectl helm; do
  command -v "$cmd" >/dev/null || { echo "missing required command: $cmd" >&2; exit 2; }
done

node="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl label node "$node" run.ai/simulated-gpu-node-pool=default --overwrite
# Simulate the product label normally published by NVIDIA GPU discovery so the
# H100 ResourceFlavor exercises the same node-selection contract in kind.
kubectl label node "$node" nvidia.com/gpu.product=NVIDIA-H100-80GB-HBM3 --overwrite
kubectl create namespace "$FAKE_GPU_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace "$FAKE_GPU_NAMESPACE" pod-security.kubernetes.io/enforce=privileged --overwrite

helm upgrade --install "$FAKE_GPU_RELEASE" "$FAKE_GPU_CHART" \
  --namespace "$FAKE_GPU_NAMESPACE" \
  --version "$FAKE_GPU_VERSION" \
  --wait --timeout "${TIMEOUT_SECONDS}s"

# Keep the acceptance contract independent of chart-internal topology value
# names: FGO's default node-pool is selected by the node label above. The
# observable contract we require is nvidia.com/gpu becoming allocatable.

deadline=$((SECONDS + TIMEOUT_SECONDS))
while (( SECONDS < deadline )); do
  allocatable="$(kubectl get node "$node" -o jsonpath='{.status.allocatable.nvidia\.com/gpu}' 2>/dev/null || true)"
  if [[ -n "$allocatable" && "$allocatable" != "0" ]]; then
    # fake-gpu-operator publishes its own simulated product label (for example
    # Tesla-K80). Override it only after the operator is ready so the kind node
    # represents the H100 flavor exercised by this acceptance test.
    # The operator may perform a final asynchronous label reconciliation after
    # the extended resource first becomes allocatable. Keep the test fixture
    # converged on the production-like H100 product label for a short stability
    # window before handing the node to the Golden Path.
    stable=0
    while (( stable < 5 )); do
      kubectl label node "$node" nvidia.com/gpu.product=NVIDIA-H100-80GB-HBM3 --overwrite >/dev/null
      sleep 2
      product="$(kubectl get node "$node" -o jsonpath='{.metadata.labels.nvidia\.com/gpu\.product}')"
      if [[ "$product" == "NVIDIA-H100-80GB-HBM3" ]]; then
        stable=$((stable + 1))
      else
        stable=0
      fi
    done
    echo "Fake GPU ready: node=$node nvidia.com/gpu=$allocatable product=NVIDIA-H100-80GB-HBM3"
    exit 0
  fi
  sleep 2
done

kubectl get node "$node" -o yaml >&2
kubectl get pods -n "$FAKE_GPU_NAMESPACE" -o wide >&2 || true
echo "fake GPU did not become allocatable" >&2
exit 1
