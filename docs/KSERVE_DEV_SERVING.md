# KServe v1beta1 custom predictor: dev-only create/read flow

**Scope:** externally provisioned, operator-authorized KServe cluster. This is
not a GPUPanel Serving controller or an accepted platform Serving provider.

The intent is a Kubernetes-native `serving.kserve.io/v1beta1`
`InferenceService` using KServe's documented **custom predictor containers**,
with a single digest-pinned inference runtime container and explicit whole-GPU
requests/limits. It does not provision a model store, GPU driver, Gateway,
llm-d InferencePool, autoscaler, Secrets, namespace, or KServe installation.

Minimum example (replace the sha256 digest with a REAL image):

```json
{
  "apiVersion": "serving.kserve.io/v1beta1",
  "kind": "InferenceService",
  "metadata": {
    "name": "model-a",
    "namespace": "research",
    "annotations": {"ai.compute/project-id": "research"}
  },
  "spec": {"predictor": {
    "minReplicas": 1, "maxReplicas": 1,
    "containers": [{
      "name": "kserve-container",
      "image": "registry.example/llm@sha256:REPLACE_WITH_64_REAL_LOWERCASE_HEX_DIGITS",
      "args": ["--model", "/models/a"],
      "ports": [{"containerPort": 8000, "protocol": "TCP"}],
      "resources": {
        "requests": {"cpu": "1", "memory": "16Gi", "nvidia.com/gpu": 1},
        "limits": {"cpu": "1", "memory": "16Gi", "nvidia.com/gpu": 1}
      }
    }]
  }}
}
```

The runtime image must supply its own valid entrypoint and interpret `args`.
No model path is auto-mounted in this slice; any required model files must
already be built into the image or supplied by the independently configured
runtime. Only use a trusted internal deployment.

```bash
# Validates manifest and emits plan only. No network access.
python3 scripts/dev/kserve_service.py serving.json

# kubectl CREATE only; existing objects are never overwritten.
python3 scripts/dev/kserve_service.py serving.json --context kind-gpu --create

# GET only: reports KServe controller's Ready condition or an unproven state.
python3 scripts/dev/kserve_service.py serving.json --context kind-gpu --status
```

**Security and acceptance:** namespace and project annotations are operator
claims, NOT proof of a ProjectBinding or tenant permission. Check that the
namespace and Kubernetes RBAC are scoped and KServe is installed. A successful
CREATE does not prove readiness. A KServe controller Ready=True also does not
prove HTTP routing, model accuracy, token performance, Gateway/llm-d health,
artifact integrity or end-user inference. A follow-up live HTTP request with
approved access path and exact model output/evidence is REQUIRED for release.

Kueue remains the sole production v0.1 queued training/batch Workload path.
Serving is a distinct **external KServe operator** lifecycle and is not
promoted into the v0.1 accepted capability matrix.

Upstream schema: https://kserve.github.io/website/docs/0.16/model-serving/predictive-inference/frameworks/custom-predictor
and https://github.com/kserve/kserve/blob/master/docs/samples/v1beta1/custom/simple.yaml
