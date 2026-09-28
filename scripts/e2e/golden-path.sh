#!/usr/bin/env bash
set -euo pipefail

: "${BASE_URL:=http://127.0.0.1:8000}"
: "${E2E_EMAIL:=golden-path@example.invalid}"
: "${E2E_PASSWORD:=GoldenPath-Only-123!}"
: "${TIMEOUT_SECONDS:=180}"

need() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 2; }; }
for cmd in curl python kubectl; do need "$cmd"; done

json() { python -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
request() {
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -fsS -X "$method" "$BASE_URL$path" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body"
  else
    curl -fsS -X "$method" "$BASE_URL$path" -H "Authorization: Bearer $TOKEN"
  fi
}
wait_status() {
  local path="$1" expression="$2" deadline=$((SECONDS + TIMEOUT_SECONDS))
  while (( SECONDS < deadline )); do
    payload="$(request GET "$path")"
    if PAYLOAD="$payload" EXPR="$expression" python - <<'PY'
import json, os
data=json.loads(os.environ["PAYLOAD"])
expr=os.environ["EXPR"]
status=data.get("status") or {}
if expr == "pool-ready":
    ok=status.get("phase")=="ready" and any(c.get("type")=="Ready" and c.get("status") is True for c in status.get("conditions",[]))
elif expr == "workload-ready":
    provider=status.get("provider_status") or {}
    ok=(status.get("admitted") is True and status.get("phase")=="ready"
        and int(provider.get("succeeded", 0) or 0) >= 1)
else:
    ok=False
raise SystemExit(0 if ok else 1)
PY
    then echo "$payload"; return 0; fi
    sleep 2
  done
  echo "timeout waiting for $expression: $path" >&2
  return 1
}

curl -fsS "$BASE_URL/healthz" >/dev/null
curl -fsS -X POST "$BASE_URL/auth/register" -H 'Content-Type: application/json'   -d "{\"email\":\"$E2E_EMAIL\",\"password\":\"$E2E_PASSWORD\"}" >/dev/null || true
TOKEN="$(curl -fsS -X POST "$BASE_URL/auth/jwt/login" -H 'Content-Type: application/x-www-form-urlencoded'   --data-urlencode "username=$E2E_EMAIL" --data-urlencode "password=$E2E_PASSWORD" | json access_token)"

tenant="$(request POST /api/v1/tenants '{"name":"golden-path"}')"
TENANT_ID="$(printf '%s' "$tenant" | json id)"
PROJECT_ID="$(printf '%s' "$tenant" | python -c 'import json,sys; print(json.load(sys.stdin)["default_project"]["id"])')"

kubectl create namespace golden-path --dry-run=client -o yaml | kubectl apply -f -

pool_path="/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/compute-pools"
request POST "$pool_path" '{
  "name":"cpu-golden",
  "binding":{
    "namespace":"golden-path",
    "local_queue":"golden",
    "cluster_queue":"golden",
    "flavors":[{"name":"cpu","accelerator_class":"cpu","resource_name":"cpu"}],
    "quotas":[{"resource":"cpu","nominal_quota":4}]
  }
}' >/dev/null
wait_status "$pool_path/cpu-golden" pool-ready >/dev/null

workload_path="/api/v1/tenants/$TENANT_ID/projects/$PROJECT_ID/workloads"
request POST "$workload_path" '{
  "name":"hello",
  "kind":"batch",
  "compute_pool":{"name":"cpu-golden"},
  "accelerator":{"class_name":"cpu","count":1},
  "image":"busybox:1.36",
  "command":["sh","-c","echo golden-path && sleep 3"]
}' >/dev/null
result="$(wait_status "$workload_path/hello" workload-ready)"
printf '%s\n' "$result"
echo "Golden Path PASS: Project -> Pool -> Workload -> Admitted -> Pods/Job Ready -> ObservedState"
