#!/usr/bin/env bash
set -euo pipefail

: "${BASE_URL:=http://127.0.0.1:8000}"
: "${E2E_PASSWORD:=GoldenPath-Only-123!}"
: "${TIMEOUT_SECONDS:=240}"
: "${RUN_ID:=$(date +%s)}"
: "${E2E_EMAIL:=gpu-golden-${RUN_ID}@example.com}"

need() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 2; }; }
for cmd in curl python kubectl; do need "$cmd"; done

json() { python -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
request() {
  local method="$1" path="$2" body="${3:-}"
  local -a args=(-fsS -X "$method" "$BASE_URL$path" -H "Authorization: Bearer $TOKEN")
  [[ -z "$body" ]] || args+=(-H 'Content-Type: application/json' -d "$body")
  curl "${args[@]}"
}
wait_status() {
  local path="$1" expression="$2" deadline=$((SECONDS + TIMEOUT_SECONDS))
  while (( SECONDS < deadline )); do
    payload="$(request GET "$path")"
    if PAYLOAD="$payload" EXPR="$expression" python - <<'PY'
import json, os
status=(json.loads(os.environ["PAYLOAD"]).get("status") or {})
conditions=status.get("conditions", [])
if os.environ["EXPR"] == "pool-ready":
    ok=status.get("phase")=="ready" and any(c.get("type")=="Ready" and c.get("status") is True for c in conditions)
else:
    provider=status.get("provider_status") or {}
    ok=status.get("admitted") is True and status.get("phase")=="ready" and int(provider.get("succeeded",0) or 0)>=1
raise SystemExit(0 if ok else 1)
PY
    then printf '%s' "$payload"; return 0; fi
    sleep 2
  done
  echo "--- platform status: $path ---" >&2
  request GET "$path" >&2 || true
  echo >&2
  echo "--- ClusterQueue conditions ---" >&2
  kubectl get clusterqueue "a100-golden-$RUN_ID" -o jsonpath='{.status.conditions}' >&2 || true
  echo >&2
  echo "--- LocalQueue conditions ---" >&2
  kubectl get localqueue "a100-golden-$RUN_ID" -n golden-gpu -o jsonpath='{.status.conditions}' >&2 || true
  echo >&2
  kubectl get clusterqueues,resourceflavors -o wide >&2 || true
  kubectl get localqueues,workloads,jobs,pods -n golden-gpu -o wide >&2 || true
  return 1
}

gpu="$(kubectl get nodes -o jsonpath='{.items[0].status.allocatable.nvidia\.com/gpu}' 2>/dev/null || true)"
[[ -n "$gpu" && "$gpu" != "0" ]] || { echo "nvidia.com/gpu is not allocatable" >&2; exit 1; }

curl -fsS "$BASE_URL/healthz" >/dev/null
curl -fsS -X POST "$BASE_URL/auth/register" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$E2E_EMAIL\",\"password\":\"$E2E_PASSWORD\"}" >/dev/null
login="$(curl -fsS -X POST "$BASE_URL/auth/jwt/login" -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "username=$E2E_EMAIL" --data-urlencode "password=$E2E_PASSWORD")"
TOKEN="$(printf '%s' "$login" | json access_token)"
[[ -n "$TOKEN" ]] || { echo "login returned empty access token" >&2; exit 1; }

tenant="$(request POST /api/v1/tenants "{\"name\":\"gpu-golden-$RUN_ID\"}")"
TENANT_ID="$(printf '%s' "$tenant" | json id)"
PROJECT_ID="$(printf '%s' "$tenant" | python -c 'import json,sys; print(json.load(sys.stdin)["default_project"]["id"])')"
kubectl create namespace golden-gpu --dry-run=client -o yaml | kubectl apply -f -

pool_path="/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/compute-pools"
pool_payload="{\"name\":\"a100-golden-$RUN_ID\",\"binding\":{\"namespace\":\"golden-gpu\",\"local_queue\":\"a100-golden-$RUN_ID\",\"cluster_queue\":\"a100-golden-$RUN_ID\",\"flavors\":[{\"name\":\"a100-80g-$RUN_ID\",\"accelerator_class\":\"a100-80g\",\"resource_name\":\"nvidia.com/gpu\"}],\"quotas\":[{\"resource\":\"nvidia.com/gpu\",\"nominal_quota\":4}]}}"
request POST "$pool_path" "$pool_payload" >/dev/null
wait_status "$pool_path/a100-golden-$RUN_ID" pool-ready >/dev/null

workload_path="/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/workloads"
workload_payload="{\"name\":\"gpu-smoke-$RUN_ID\",\"kind\":\"batch\",\"compute_pool\":{\"name\":\"a100-golden-$RUN_ID\"},\"accelerator\":{\"class_name\":\"a100-80g\",\"count\":1},\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"echo fake-gpu-golden && sleep 3\"]}"
request POST "$workload_path" "$workload_payload" >/dev/null
result="$(wait_status "$workload_path/gpu-smoke-$RUN_ID" workload-ready)"
printf '%s\n' "$result"

job="$(printf '%s' "$result" | python -c 'import json,sys; print((json.load(sys.stdin).get("status") or {}).get("provider_ref") or "")')"
[[ -n "$job" ]] || { echo "workload status did not expose provider_ref" >&2; exit 1; }
requested="$(kubectl get job "$job" -n golden-gpu -o jsonpath='{.spec.template.spec.containers[0].resources.requests.nvidia\.com/gpu}')"
[[ "$requested" == "1" ]] || { echo "expected nvidia.com/gpu request=1, got $requested" >&2; exit 1; }

echo "Fake GPU Golden PASS: a100-80g -> nvidia.com/gpu -> Kueue -> Job -> ObservedState"
