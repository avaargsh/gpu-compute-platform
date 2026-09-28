#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

command -v kind >/dev/null || { echo "kind is required" >&2; exit 2; }
command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 2; }
command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }

make kind-up
make install-kueue

kind get kubeconfig --name ai-compute > .kubeconfig-e2e
export CONTROL_PLANE_SCHEDULER_PROVIDER=kueue
export CONTROL_PLANE_KUBECONFIG="$ROOT/.kubeconfig-e2e"

docker compose up -d postgres redis
make compose-migrate

# Run API/worker on the host so the kind kubeconfig loopback endpoint remains
# reachable. Docker containers cannot reliably use kind's 127.0.0.1 API server.
export DATABASE_URL="postgresql+asyncpg://postgres:postgres@127.0.0.1:5432/gpu_platform"
export CELERY_BROKER_URL="redis://127.0.0.1:6379/0"
export CELERY_RESULT_BACKEND="redis://127.0.0.1:6379/0"
export REDIS_URL="redis://127.0.0.1:6379/0"

uv run uvicorn app.main:app --host 127.0.0.1 --port 8000 > /tmp/compute-api.log 2>&1 &
api_pid=$!
uv run celery -A app.core.celery_app.celery_app worker -Q control_plane -l info --concurrency=2 > /tmp/compute-worker.log 2>&1 &
worker_pid=$!
trap 'kill "$api_pid" "$worker_pid" 2>/dev/null || true' EXIT

deadline=$((SECONDS + 120))
until curl -fsS http://127.0.0.1:8000/healthz >/dev/null 2>&1; do
  if (( SECONDS >= deadline )); then
    docker compose ps
    tail -150 /tmp/compute-api.log || true
    tail -150 /tmp/compute-worker.log || true
    exit 1
  fi
  sleep 2
done

make e2e-golden
