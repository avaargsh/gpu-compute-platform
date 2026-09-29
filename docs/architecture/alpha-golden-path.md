# Alpha Golden Path

The alpha is intentionally narrow. New providers and secondary features do not block alpha.

## Exit criteria

The supported compute path is:

```text
Project
  -> ComputePool
  -> Workload
  -> Kueue admission
  -> Pods Ready
  -> ObservedState
```

A release is alpha-ready only when this path is repeatable, generation-safe, observable, and covered by contract/integration tests.

## Scheduling policy

Users request compute intent; they do not select a Kubernetes scheduler.

| Workload profile | ComputePool policy | Admission | Placement |
| --- | --- | --- | --- |
| single GPU / small jobs | standard | Kueue | kube-scheduler |
| distributed / gang | gang | Kueue | Volcano |

The ComputePool owns scheduler policy and provider bindings. Workload remains portable.

## Accelerator portability

Workload requests only a portable class and count:

```yaml
accelerator:
  class_name: h100
  count: 8
```

ComputePool resolves the class to ResourceFlavor, concrete resource name, and optional DeviceClass/HAMi policy. Provider resource names never enter the public Workload contract.

## Serving golden path

The next supported serving path is:

```text
ModelRevision
  -> Deployment
  -> KServe LLMInferenceService
  -> llm-d
  -> vLLM
  -> OpenAI-compatible Endpoint
```

## Observability before breadth

Before adding providers, the control plane must expose:

- Conditions with reason and observed generation.
- desired generation vs observed generation drift.
- reconcile failure evidence.
- admission state and pod readiness.
- stable provider references without leaking provider mechanics into desired state.

## Non-goals for alpha

- broad cloud-provider coverage
- user-selected schedulers
- arbitrary provider-specific accelerator fields
- additional serving backends before the golden serving path is stable
