#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

for cmd in kind kubectl docker helm uv curl; do
  command -v "$cmd" >/dev/null || { echo "$cmd is required" >&2; exit 2; }
done

make kind-up
make install-kueue
make install-fake-gpu

kind get kubeconfig --name ai-compute > .kubeconfig-e2e
export CONTROL_PLANE_SCHEDULER_PROVIDER=kueue
export CONTROL_PLANE_KUBECONFIG="$ROOT/.kubeconfig-e2e"
export DATABASE_URL="postgresql+asyncpg://postgres:postgres@127.0.0.1:5432/gpu_platform"
export CELERY_BROKER_URL="redis://127.0.0.1:6379/0"
export CELERY_RESULT_BACKEND="redis://127.0.0.1:6379/0"
export REDIS_URL="redis://127.0.0.1:6379/0"

docker compose up -d postgres redis
make migrate

uv run uvicorn app.main:app --host 127.0.0.1 --port 8000 > /tmp/compute-api.log 2>&1 &
api_pid=$!
uv run celery -A app.core.celery_app.celery_app worker -Q control_plane -l info --concurrency=2 > /tmp/compute-worker.log 2>&1 &
worker_pid=$!
trap 'kill "$api_pid" "$worker_pid" 2>/dev/null || true' EXIT

deadline=$((SECONDS + 120))
until curl -fsS http://127.0.0.1:8000/healthz >/dev/null 2>&1; do
  if (( SECONDS >= deadline )); then
    tail -150 /tmp/compute-api.log || true
    tail -150 /tmp/compute-worker.log || true
    exit 1
  fi
  sleep 2
done

make e2e-gpu-golden
