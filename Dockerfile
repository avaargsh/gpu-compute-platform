# AI Compute Control Plane image.
# Phase 0 deliberately has no frontend or CUDA dependency. Accelerator runtime
# integration belongs to Kubernetes nodes/providers, not the control-plane API.

FROM python:3.12-slim AS python-base

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    git \
    build-essential \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir uv

RUN useradd -m -u 1000 appuser && mkdir -p /app && chown -R appuser:appuser /app
USER appuser
WORKDIR /app

FROM python-base AS deps
COPY --chown=appuser:appuser pyproject.toml README.md ./
RUN uv venv .venv && . .venv/bin/activate && uv pip install -e .

FROM python-base AS app
COPY --from=deps --chown=appuser:appuser /app/.venv /app/.venv
COPY --chown=appuser:appuser app/ ./app/
COPY --chown=appuser:appuser alembic/ ./alembic/
COPY --chown=appuser:appuser alembic.ini ./
COPY --chown=appuser:appuser main.py ./
COPY --chown=appuser:appuser scripts/ ./scripts/
COPY --chown=appuser:appuser examples/ ./examples/
COPY --chown=appuser:appuser tests/ ./tests/
COPY --chown=appuser:appuser pytest.ini ./

RUN find scripts -type f -name '*.sh' -exec chmod +x {} +

ENV PATH="/app/.venv/bin:$PATH" \
    PYTHONPATH="/app" \
    ENVIRONMENT="production" \
    DATABASE_URL="postgresql+asyncpg://postgres:postgres@postgres:5432/gpu_platform" \
    CELERY_BROKER_URL="redis://redis:6379/0" \
    CELERY_RESULT_BACKEND="redis://redis:6379/0" \
    REDIS_URL="redis://redis:6379/0"

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=5 \
    CMD curl -f http://localhost:8000/healthz || exit 1

EXPOSE 8000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
