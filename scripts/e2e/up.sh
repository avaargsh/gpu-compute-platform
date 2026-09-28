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
docker compose up -d app celery-worker

deadline=$((SECONDS + 120))
until curl -fsS http://127.0.0.1:8000/healthz >/dev/null 2>&1; do
  if (( SECONDS >= deadline )); then
    docker compose ps
    docker compose logs --tail=150 app celery-worker
    exit 1
  fi
  sleep 2
done

make e2e-golden
