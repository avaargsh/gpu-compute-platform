# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

Project: GPU Compute Platform (FastAPI-based)

- Purpose: MVP for a GPU compute platform with authentication, user management, and a pluggable GPU provider layer.
- Key tech: FastAPI, SQLAlchemy (async), Alembic, FastAPI Users (JWT), pytest, uv.

Commands

- Install dependencies
  - uv sync
- Database migrations (SQLite by default)
  - uv run alembic upgrade head
- Run dev server
  - Option A: uv run python run_dev.py
  - Option B: uv run uvicorn app.main:app --reload --host 0.0.0.0 --port 8000
- Run all tests
  - uv run pytest
- Run a single test file (example)
  - uv run pytest tests/test_auth.py -v
- Run GPU provider tests specifically
  - uv run pytest tests/test_gpu_providers.py -v
- Run comprehensive GPU provider tests
  - uv run pytest tests/test_gpu_comprehensive.py -v
- Generate test coverage report
  - uv run pytest --cov=app --cov-report=html

Entrypoints and API docs

- Local docs: http://localhost:8000/docs (Swagger) and http://localhost:8000/redoc
- Health check: http://localhost:8000/health

High-level architecture

- app/main.py
  - FastAPI app factory with lifespan to initialize database tables on startup.
  - Routers mounted under /auth (authentication) and /api (protected endpoints).
  - CORS policy uses app.core.config.Settings.allowed_origins.
- app/core
  - config.py: Settings via pydantic-settings (reads .env). Holds app_name, version, DB URL, security values.
  - database.py: Async SQLAlchemy engine/session setup and create_db_and_tables.
  - auth.py: FastAPI Users integration (current_active_user/current_superuser dependencies) used by protected routes.
- app/models
  - user.py: SQLAlchemy model extending FastAPI Users base with custom profile/usage fields.
- app/api
  - auth.py: Auth routes (registration/login/JWT) using FastAPI Users.
  - protected.py: Example authenticated and admin-only endpoints.
- app/gpu (Unified GPU Provider Layer)
  - interface.py: Unified contracts and DTOs for GPU providers
    - JobStatus, GpuSpec, JobConfig, JobResult, CostInfo
    - GpuProviderInterface with submit_job, get_job_status, cancel_job, get_job_logs, get_cost_info, list_available_gpus, health_check
  - providers/
    - alibaba.py: Adapter for Alibaba Cloud ECS (IaaS-centric). Submits jobs by provisioning GPU ECS instances and running containerized workload via user-data.
    - tencent.py: Adapter for Tencent Cloud TKE (Kubernetes-centric). Submits jobs as Kubernetes Jobs with GPU resource requests.

GPU Provider design notes

- Application layer calls a provider instance via the unified interface; no cloud-specific logic leaks upward.
- Alibaba adapter maps our GpuSpec to ECS instance types and bootstraps Docker + NVIDIA runtime via user-data; status approximated from instance state (Pending/Running/Stopped). Logs/costs would typically require extra integrations (SSH/CloudMonitor/Billing APIs).
- Tencent adapter uses Kubernetes Jobs in a dedicated namespace (gpu-jobs), maps our GpuSpec to nvidia.com/gpu requests, and pulls status/logs via Kubernetes API. Real cost requires Tencent billing APIs and usage metrics.

Local development conventions

- Python version >= 3.12, managed via uv and pyproject.toml.
- Tests configured via pytest.ini with asyncio_mode=auto.

Provider usage (example pseudo-code)

```python path=null start=null
from app.gpu.interface import JobConfig, GpuSpec
from app.gpu.providers.tencent import TencentCloudAdapter

provider = TencentCloudAdapter({
    "secret_id": "{{TENCENT_SECRET_ID}}",
    "secret_key": "{{TENCENT_SECRET_KEY}}",
    "region": "ap-shanghai",
    "cluster_id": "cls-xxxxxx",
    # optional kubeconfig (base64)
})

job_id = await provider.submit_job(JobConfig(
    name="resnet50-train",
    image="nvcr.io/nvidia/pytorch:24.02-py3",
    command=["python", "train.py", "--epochs", "1"],
    gpu_spec=GpuSpec(gpu_type="A100", gpu_count=1, memory_gb=40, vcpus=12, ram_gb=48),
))
status = await provider.get_job_status(job_id)
logs = await provider.get_job_logs(job_id, lines=200)
```

Notes pulled from README.md

- Use uv for dependency and script execution.
- After sync, run alembic upgrade head, then start dev server.
- Auth endpoints:
  - POST /auth/register, POST /auth/jwt/login, POST /auth/jwt/logout
- User routes:
  - GET/PATCH /auth/users/me
- Protected examples:
  - GET /api/protected-route, GET /api/admin-only

Repository tips for Warp

- When adding new cloud providers, implement app/gpu/providers/<provider>.py by subclassing GpuProviderInterface in app/gpu/interface.py.
- Avoid editing code via shell redirections; use structured patches to keep diffs clear.
- Keep secrets out of code; pass via environment or .env consumed by Settings.

