# AI Compute Control Plane v2

## Purpose

Evolve the project from a multi-cloud GPU job MVP into a Kubernetes-native AI compute control plane.

The control plane owns user-facing intent, policy, lifecycle, status and economics. It delegates admission, placement, device allocation and model-serving mechanics to specialized providers instead of implementing a second scheduler inside the platform.

## Domain model

```text
Tenant / Project
      |
  ComputePool -------- AcceleratorClass
      |
   Workload
   |-- TrainingJob
   |-- BatchJob
   `-- Workspace
      |
    Model -------- ModelRevision
      |
 ServingConfig
      |
  Deployment -------- Endpoint
```

### ComputePool

A logical pool of accelerator capacity available to one or more projects.

It describes policy and eligibility, not individual GPU IDs. A pool may map to a Kubernetes cluster, queue/cohort, node pool, cloud capacity pool, or another provider-specific capacity boundary.

### AcceleratorClass

Portable accelerator requirements such as vendor, model/family, memory class and sharing/isolation capabilities.

Provider-specific resource names and node labels belong in provider bindings rather than application-facing workload objects.

### Workload

A desired compute workload. Initial workload kinds are TrainingJob, BatchJob and Workspace.

A workload requests resources and references a ComputePool. It must not persist scheduler-selected physical GPU IDs as desired state.

### Model / ModelRevision

Model is the logical registry object. ModelRevision is an immutable or versioned artifact reference produced by training/import and consumed by serving.

### ServingConfig

Portable serving intent:

- runtime: vLLM, SGLang, or another runtime provider
- accelerator requirements
- tensor/pipeline/expert parallelism
- quantization
- model/runtime arguments
- autoscaling and SLO hints

### Deployment

Desired model-serving release. Deployment references ModelRevision and ServingConfig; it does not directly represent a Pod, container, node or GPU device.

### Endpoint

Stable traffic identity in front of one or more deployments. Endpoint owns traffic policy, rollout/shadow policy, rate limits and externally visible serving status.

## Provider boundaries

```text
Control Plane
   |
   +-- SchedulerProvider
   |     +-- Kueue
   |     +-- Volcano
   |     `-- KAI
   |
   +-- DeviceProvider
   |     +-- Kubernetes DRA
   |     +-- HAMi
   |     `-- legacy extended resources
   |
   +-- ServingProvider
   |     +-- KServe / LLMInferenceService
   |     +-- llm-d
   |     `-- native Kubernetes compatibility provider
   |
   `-- RuntimeProvider
         +-- vLLM
         `-- SGLang
```

Provider bindings translate portable desired state into provider-specific resources. Provider implementation details must not leak into the core domain model.

## Ownership boundaries

The platform SHOULD own:

- tenant/project identity and authorization
- desired workload and serving state
- model/revision lifecycle
- quota and budget policy
- provider bindings
- normalized status
- usage/cost accounting
- SLO/evidence surfaces

The platform SHOULD delegate:

- queue admission and quota borrowing to Kueue or equivalent
- gang scheduling and topology-aware placement to a scheduler
- physical device allocation to Kubernetes device mechanisms/providers
- distributed workload orchestration to established workload controllers
- inference replica/routing mechanics to serving providers

## Migration strategy

This is an incremental migration, not a rewrite.

1. Introduce the portable domain objects and provider interfaces without replacing existing APIs.
2. Wrap existing Kubernetes and cloud adapters as compatibility providers.
3. Move queue admission, placement and device allocation behind provider interfaces.
4. Introduce ModelRevision -> ServingConfig -> Deployment -> Endpoint as the serving path.
5. Add modern inference providers while retaining the native Kubernetes path as fallback.
6. Remove compatibility fields only after their consumers have migrated.

## Security baseline

- Never put object-storage or cloud long-lived credentials directly in workload specs or Pod environment variables.
- Prefer workload identity; otherwise use referenced Kubernetes Secrets or provider-native secret stores.
- Pin runtime images by version or digest for reproducible deployments.
- Keep physical GPU IDs, node names and vendor-specific labels out of portable desired state.

## Compatibility rule

Legacy providers are allowed to use provider-specific fields internally. New public APIs and domain objects must remain provider-neutral unless the field is explicitly namespaced as a provider extension.
