# AI Compute Control Plane

Kubernetes-native control plane for GPU/NPU workloads, distributed training, and model serving.

> **Status:** alpha. The previous GPU-task/multi-cloud MVP is discontinued. There is no backward-compatibility guarantee.

## API

Canonical API prefix:

```text
/api/v1
```

Resource hierarchy:

```text
Tenant
└── Project
    ├── ComputePool
    ├── Workload
    ├── ModelRevision
    ├── Deployment
    └── Endpoint
```

Current Workload endpoints:

```text
POST   /api/v1/tenants
POST   /api/v1/tenants/{tenant_id}/projects

POST   /api/v1/tenants/{tenant_id}/projects/{project_id}/workloads
GET    /api/v1/tenants/{tenant_id}/projects/{project_id}/workloads/{name}
PUT    /api/v1/tenants/{tenant_id}/projects/{project_id}/workloads/{name}
DELETE /api/v1/tenants/{tenant_id}/projects/{project_id}/workloads/{name}
```

Mutations that require provider reconciliation return `202 Accepted`. Provider job IDs, Celery task IDs, physical GPU IDs, Kubernetes node names, credentials, and scheduler-specific resource names are not part of the public resource contract.

## Architecture

```text
Client / CLI / SDK
       |
       v
Tenant -> Project
       |
       v
Desired State API
       |
       v
PostgreSQL
       |
       +---- Celery wake-up
       +---- periodic convergence sweep
       |
       v
Reconcile lease
       |
       v
Controllers
       |
       +--> Kueue ---------- admission / quota
       +--> Volcano/KAI ---- placement / gang
       +--> DRA/HAMi ------- devices / sharing
       +--> KServe/llm-d --- serving
       +--> vLLM/SGLang ---- runtime
       |
       v
Observed State / Conditions / Evidence / Revision History
```

## Invariants

- PostgreSQL is the source of truth for desired and observed platform state.
- Redis/Celery only accelerates reconciliation.
- API resources express portable user intent; providers translate that intent.
- Tenant/Project is the ownership and authorization boundary.
- Desired-state mutations increment generation.
- Observed writes are generation-aware.
- Reconcile side effects are serialized by database leases.
- Batch workload provider revisions are immutable.
- Kubernetes access is fail-closed unless explicitly configured.
- Production schema is managed by Alembic, never application startup `create_all()`.

## Golden path

```text
Project
 -> ComputePool
 -> Workload
 -> Kueue admission
 -> scheduler placement
 -> DRA/HAMi allocation
 -> Kubernetes execution
 -> ObservedState / Evidence
```

Serving:

```text
ModelRevision
 -> Deployment
 -> KServe / llm-d
 -> vLLM / SGLang
 -> InferencePool
 -> Gateway API
 -> OpenAI-compatible endpoint
```

## Development

Requires Python 3.12+ and `uv`.

```bash
uv sync --frozen
uv run alembic upgrade head
uv run pytest -q
```

Phase 0 acceptance path:

```bash
make kind-up
make install-kueue
make migrate

# Start PostgreSQL/Redis, the API and Celery worker with:
# CONTROL_PLANE_SCHEDULER_PROVIDER=kueue
# CONTROL_PLANE_KUBECONFIG=<path to the kind kubeconfig>
make e2e-golden
```

The Golden Path intentionally uses CPU as a fake accelerator so Phase 0 validates
control-plane admission and convergence without requiring a GPU node. HAMi/DRA
are Phase 1 concerns.

The scheduler is disabled by default. To use Kueue:

```bash
export CONTROL_PLANE_SCHEDULER_PROVIDER=kueue
export CONTROL_PLANE_KUEUE_NAMESPACE=team-a
export CONTROL_PLANE_KUEUE_LOCAL_QUEUE=training
export CONTROL_PLANE_ACCELERATOR_RESOURCE=nvidia.com/gpu
```

## Project direction

The repository is a clean-break control-plane implementation. Legacy GPU job APIs, DAG APIs, multi-cloud job providers, and their task-state model are not part of the application surface and will be removed as the v1 control plane reaches feature parity with the new architecture.
