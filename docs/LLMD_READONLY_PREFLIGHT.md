# llm-d v0.10 / GAIE v1 read-only serving preflight

Status: **integration experiment, NOT a production Serving provider**.

This slice reuses the **Gateway API Inference Extension (GAIE) v1 `InferencePool`**
already used by llm-d v0.10 instead of implementing a second routing layer.
It does **not** install llm-d/KServe, write any Kubernetes objects, add a
Serving domain object, edit the Cluster Agent runtime, or modify the frozen
Kueue-only v0.1 Golden Path.

## What is verified

The standalone preflight checks, through the existing Kubernetes client:

1. the API server advertises **namespaced** `inferencepools` in
   `inference.networking.k8s.io/v1`;
2. a specifically named `InferencePool` exists in the requested namespace;
3. the returned object has the expected API version, kind, namespace/name,
   nonempty UID and resourceVersion, positive generation, and is not deleting.

The machine-readable result is `OBSERVED_ONLY`, **not** `Ready` / `Admitted`
/ `QuotaApplied` / `ServingAvailable`. A GAIE `InferencePool` can be
managed by providers other than llm-d. Its existence proves **neither**
llm-d ownership nor that an Endpoint Picker, Gateway, HTTPRoute, model server
or GPU is ready. There is no provider registration or automatic promotion.

## Usage (read-only)

Use a pre-existing, intentionally provisioned cluster with llm-d v0.10 or a
GAIE v1-compatible installation. Ensure `KUBECONFIG` or in-cluster auth allows
API discovery and `get` on the named InferencePool.

```bash
go run ./cmd/llmd-preflight --namespace serving --pool model-a
```

Exit codes:
- 0: the GAIE v1 pool's identity was independently observed; **not** serving-ready;
- 1: API or pool absent, or identity insufficient; fail closed;
- 2: configuration, API/RBAC/transport error or malformed request; fail closed.

The command prints JSON to stdout. Example (illustrative, not a live receipt):

```json
{
  "apiVersion": "inference.networking.k8s.io/v1",
  "namespace": "serving",
  "name": "model-a",
  "apiServed": true,
  "objectObserved": true,
  "uid": "example-uid",
  "resourceVersion": "42",
  "generation": 3,
  "status": "OBSERVED_ONLY",
  "reason": "GAIEInferencePoolObservedNotServingProof",
  "servingReadyProven": false
}
```

Negative checks live in `internal/integration/llmd/preflight_test.go`:

```bash
go test ./internal/integration/llmd -count=1
go test -race ./internal/integration/llmd -count=1
make fmt-check
make vet
```

## Before any actual llm-d Serving integration

- Keep Kueue the only production execution path until existing Stage B gates
  close. This experiment does not add a `provider.Adapter`.
- Pin and review the **actual** upstream llm-d chart/image digests and Gateway
  / GAIE CRD versions (no `latest` in a production manifest).
- Run a controlled cluster test retaining identity, Gateway/HTTPRoute/EPP,
  endpoints, a real inference request, and the response/latency evidence.
- Prove generation-safe rollout, lease-fenced observation, restart/lost-ACK,
  deletion recovery and real-GPU acceptance before adding a Serving support
  row to `CAPABILITY_RELEASE_MATRIX.md`.
- Do not claim that HTTP 200, successful Helm install or an InferencePool GET
  proves model correctness, inference throughput or operation ownership.

## Authoritative upstream contracts

- llm-d v0.10 API: https://llm-d.ai/docs/api-reference
- llm-d artifacts: https://llm-d.ai/docs/api-reference/artifacts
- GAIE v1 InferencePool:
  https://gateway-api-inference-extension.sigs.k8s.io/
- Platform stage boundaries: `docs/CAPABILITY_RELEASE_MATRIX.md`
- Existing provider invariants: `docs/PROVIDER_ADAPTER_SPI.md`
