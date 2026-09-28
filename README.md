# AI Compute Control Plane

A Kubernetes-native control plane for GPU/NPU workloads and model serving.

> **Status:** active v2 development. The previous multi-cloud GPU-task MVP is retained only as a compatibility surface and receives no new control-plane features.

## Architecture

```text
Developer / API / SDK
        |
        v
Tenant -> Project
        |
        v
Desired State
Workload / Deployment / Endpoint / ComputePool
        |
        v
Reconcile Controller
        |
        +--> Kueue ---------- admission / quota
        +--> Volcano/KAI ---- placement / gang scheduling
        +--> DRA/HAMi ------- device allocation / sharing
        +--> KServe/llm-d --- serving
        +--> vLLM/SGLang ---- model runtime
        |
        v
Observed State + Conditions + Evidence + Revision History
```

## Control-plane invariants

- PostgreSQL owns desired and observed platform state.
- Redis/Celery is wake-up/delivery infrastructure, never the source of truth.
- Kubernetes providers own execution mechanics; the public domain owns user intent.
- Provider resource names, node identity, physical GPU IDs, credentials, and kubeconfig do not enter portable desired state.
- Every desired-state mutation increments a generation.
- Stale generations cannot overwrite current observed state.
- Batch workload revisions are immutable provider resources.
- Tenant/Project is the canonical ownership boundary.

## Golden path

```text
Project
  -> ComputePool
  -> Workload
  -> Kueue admission
  -> Scheduler placement
  -> DRA/HAMi device allocation
  -> Kubernetes execution
  -> ObservedState / Evidence
```

Serving evolves independently:

```text
ModelRevision
  -> Deployment
  -> KServe / llm-d
  -> vLLM / SGLang
  -> InferencePool
  -> Gateway API
  -> OpenAI-compatible Endpoint
```

## Development

Python 3.12+ and `uv` are recommended.

```bash
uv sync --frozen
uv run pytest -q
```

The control plane is fail-closed by default. It does not access Kubernetes unless a scheduler provider is explicitly configured.

```bash
export CONTROL_PLANE_SCHEDULER_PROVIDER=kueue
export CONTROL_PLANE_KUEUE_NAMESPACE=team-a
export CONTROL_PLANE_KUEUE_LOCAL_QUEUE=training
export CONTROL_PLANE_ACCELERATOR_RESOURCE=nvidia.com/gpu
```

## Repository layout

- `app/control_plane/` — portable domain, reconcile kernel and provider boundaries.
- `app/models/control_plane_*.py` — desired/observed and revision persistence.
- `app/api/v2_workloads.py` — v2 desired-state API.
- `docs/architecture/control-plane-v2.md` — architecture details.
- `app/gpu/`, legacy GPU task APIs and DAG code — compatibility layer only.

## Legacy policy

The original GPU-task/multi-cloud MVP is frozen. New functionality must not depend on `GpuTask`, cloud-specific job state, Celery task state, or legacy provider abstractions. Compatibility adapters may translate legacy requests into v2 desired state, but the dependency direction must never reverse.

## Contributing

Current priorities are controller correctness, tenancy/project isolation, provider conformance tests, Kubernetes integration tests, serving-plane reconciliation, observability/evidence, and reproducible local development.
