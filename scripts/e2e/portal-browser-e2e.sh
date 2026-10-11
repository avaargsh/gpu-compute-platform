#!/usr/bin/env bash
# Real browser -> Vite localhost proxy -> real Go HTTP server -> in-memory store.
# Intentionally NO Kubernetes/GPU fixtures and NO public bind.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:8080
export PORTAL_OPERATOR_CLUSTER_ID=portal-ci
export PORTAL_OPERATOR_PROJECT_IDS=research,serving
export PORTAL_OPERATOR_TOKEN="$(openssl rand -hex 32)"

TMP_LOG="$(mktemp -d)"
GO_PID=""
VITE_PID=""
cleanup() {
  if [[ -n "$VITE_PID" ]]; then kill "$VITE_PID" 2>/dev/null || true; wait "$VITE_PID" 2>/dev/null || true; fi
  if [[ -n "$GO_PID" ]]; then kill "$GO_PID" 2>/dev/null || true; wait "$GO_PID" 2>/dev/null || true; fi
  rm -rf "$TMP_LOG"
}
trap cleanup EXIT

go build -o "$TMP_LOG/control-plane" ./cmd/control-plane
"$TMP_LOG/control-plane" >"$TMP_LOG/go.log" 2>&1 &
GO_PID=$!
for attempt in $(seq 1 120); do
  if curl -fsS --max-time 1 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then break; fi
  if ! kill -0 "$GO_PID" 2>/dev/null; then cat "$TMP_LOG/go.log" >&2; exit 1; fi
  if [[ "$attempt" == 120 ]]; then cat "$TMP_LOG/go.log" >&2; exit 1; fi
  sleep 1
done

api=http://127.0.0.1:8080/api/v1
json_request() {
  local method="$1" endpoint="$2" body="$3"
  curl -fsS --max-time 10 -o /dev/null -X "$method" -H 'Content-Type: application/json' --data-binary "$body" "$api/$endpoint"
}

json_request POST agent/register '{"clusterId":"portal-ci","agentVersion":"e2e","kubernetesVersion":"v1.34.0","capabilities":{"kueue":true,"serving":false,"draApiAvailable":false,"providers":["kueue"],"schedulers":[{"name":"kueue","version":"v0.19.6"}]}}'
json_request PUT internal/clusters/portal-ci/desired/ComputePool/pool-aaa '{"generation":2,"spec":{"projectID":"research","accelerators":[{"class":"h100","quota":4}]}}'
# Seed ONLY disposable HTTP test data; this does NOT claim real GPU admission.
for n in $(seq -w 1 124); do
  json_request PUT "internal/clusters/portal-ci/desired/Workload/work-$n" '{"generation":2,"spec":{"projectID":"research","image":"example.invalid/e2e:fixture"}}'
done
json_request PUT internal/clusters/portal-ci/desired/Workload/secret-foreign '{"generation":2,"spec":{"projectID":"other-tenant","image":"example.invalid/e2e:fixture"}}'
json_request POST agent/reconcile-lease/claim '{"clusterId":"portal-ci","kind":"Workload","resourceId":"work-001","owner":"e2e-agent","ttlSeconds":300}'
json_request POST agent/report '{"clusterId":"portal-ci","observations":[{"kind":"Workload","id":"work-001","observedGeneration":2,"leaseOwner":"e2e-agent","evidenceRefs":["job/work-001"]}]}'

(
  cd frontend
  npm run portal:dev -- --host 127.0.0.1 >"$TMP_LOG/vite.log" 2>&1
) &
VITE_PID=$!
for attempt in $(seq 1 45); do
  if curl -fsS --max-time 1 http://127.0.0.1:3001/portal.html >/dev/null 2>&1; then break; fi
  if [[ "$attempt" == 45 ]]; then cat "$TMP_LOG/vite.log" >&2; exit 1; fi
  sleep 1
done

# Browser executes real fetches; no route mocks and no synthetic UI responses.
(cd frontend && npx playwright test --config playwright.portal.config.ts)
