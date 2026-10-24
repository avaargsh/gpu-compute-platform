#!/usr/bin/env bash
set -euo pipefail

: "${CLUSTER_ID:=kind-golden}"
: "${BASE_URL:=http://127.0.0.1:8080}"
: "${KUEUE_VERSION:=v0.19.6}"
: "${TIMEOUT_SECONDS:=240}"
: "${NAMESPACE:=golden-go}"
: "${PROJECT_ID:=project-golden}"
: "${POOL_ID:=pool-h100}"
: "${WORKLOAD_ID:=train-golden}"
: "${INVALID_WORKLOAD_ID:=train-invalid}"
: "${ACCELERATOR_RESOURCE:=nvidia.com/gpu}"
: "${ACCELERATOR_CLASS:=h100-80g}"
: "${ACCELERATOR_FLAVOR:=h100-80g}"

cleanup() {
  [[ -z "${AGENT_A_PID:-}" ]] || kill "$AGENT_A_PID" 2>/dev/null || true
  [[ -z "${AGENT_B_PID:-}" ]] || kill "$AGENT_B_PID" 2>/dev/null || true
  [[ -z "${CONTROL_PID:-}" ]] || kill "$CONTROL_PID" 2>/dev/null || true
}
trap cleanup EXIT

for cmd in curl kubectl kind helm go python; do
  command -v "$cmd" >/dev/null || { echo "missing required command: $cmd" >&2; exit 2; }
done

wait_http() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS))
  until curl -fsS "$BASE_URL/healthz" >/dev/null; do
    (( SECONDS < deadline )) || { echo "control plane did not become ready" >&2; return 1; }
    sleep 1
  done
}

wait_cluster_capabilities() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS)) payload
  while (( SECONDS < deadline )); do
    payload="$(curl -fsS "$BASE_URL/api/v1/clusters/$CLUSTER_ID/status" 2>/dev/null || true)"
    if PAYLOAD="$payload" ACCELERATOR_CLASS="$ACCELERATOR_CLASS" python - <<'PY'
import json, os
try:
    data=json.loads(os.environ["PAYLOAD"])
except Exception:
    raise SystemExit(1)
caps=data.get("capabilities") or {}
accelerators=caps.get("accelerators") or []
providers=caps.get("providers") or []
ok=(
    data.get("clusterId") is not None
    and caps.get("kueue") is True
    and providers == ["kueue"]
    and os.environ["ACCELERATOR_CLASS"] in accelerators
    and bool(data.get("kubernetesVersion"))
    and bool(data.get("lastHeartbeatAt"))
)
raise SystemExit(0 if ok else 1)
PY
    then
      printf '%s\n' "$payload"
      return 0
    fi
    sleep 1
  done
  echo "timeout waiting for registered Kueue/accelerator capabilities" >&2
  curl -sS "$BASE_URL/api/v1/clusters/$CLUSTER_ID/status" >&2 || true
  return 1
}

put() {
  local path="$1" payload="$2" response_file status
  response_file="$(mktemp)"
  if ! status="$(curl -sS -o "$response_file" -w '%{http_code}' -X PUT "$BASE_URL$path" -H 'Content-Type: application/json' -d "$payload")"; then
    printf 'PUT %s failed before receiving an HTTP status: %s\n' "$path" "$(cat "$response_file")" >&2
    rm -f "$response_file"
    return 1
  fi
  if [[ ! "$status" =~ ^2[0-9][0-9]$ ]]; then
    printf 'PUT %s returned HTTP %s: %s\n' "$path" "$status" "$(cat "$response_file")" >&2
    rm -f "$response_file"
    return 1
  fi
  rm -f "$response_file"
}

condition_true() {
  local payload="$1" type="$2"
  PAYLOAD="$payload" TYPE="$type" python - <<'PY'
import json, os
data=json.loads(os.environ["PAYLOAD"])
conditions=(data.get("status") or {}).get("conditions") or []
raise SystemExit(0 if any(c.get("type")==os.environ["TYPE"] and c.get("status")=="True" for c in conditions) else 1)
PY
}

wait_workload() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS)) payload
  while (( SECONDS < deadline )); do
    payload="$(curl -fsS "$BASE_URL/api/v1/workloads/$WORKLOAD_ID")"
    if PAYLOAD="$payload" python - <<'PY'
import json, os
data=json.loads(os.environ["PAYLOAD"])
status=data.get("status") or {}
conditions=status.get("conditions") or []
truth={c.get("type") for c in conditions if c.get("status")=="True"}
ok=(status.get("syncState")=="Synced"
    and "QuotaReserved" in truth
    and "Admitted" in truth
    and ("Ready" in truth or "Succeeded" in truth))
raise SystemExit(0 if ok else 1)
PY
    then
      printf '%s\n' "$payload"
      return 0
    fi
    sleep 2
  done
  echo "timeout waiting for Go golden workload" >&2
  kubectl get resourceflavors,clusterqueues -o wide >&2 || true
  kubectl get localqueues,workloads,jobs,pods -n "$NAMESPACE" -o wide >&2 || true
  pod="$(kubectl get pods -n "$NAMESPACE" -l "ai.compute/workload=job-$WORKLOAD_ID" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -z "$pod" ]] || kubectl describe pod "$pod" -n "$NAMESPACE" >&2 || true
  kubectl describe node "$node" >&2 || true
  [[ -z "${CONTROL_PID:-}" ]] || { echo "--- control-plane ---" >&2; cat /tmp/go-control-plane.log >&2 || true; }
  [[ -f /tmp/go-cluster-agent-a.log ]] && { echo "--- cluster-agent A ---" >&2; cat /tmp/go-cluster-agent-a.log >&2 || true; }
  [[ -f /tmp/go-cluster-agent-b.log ]] && { echo "--- cluster-agent B ---" >&2; cat /tmp/go-cluster-agent-b.log >&2 || true; }
  return 1
}

observation_owner() {
  curl -fsS "$BASE_URL/api/v1/internal/clusters/$CLUSTER_ID/state/Workload/$WORKLOAD_ID" |
    python -c 'import json,sys; print(json.load(sys.stdin).get("observationLeaseOwner", ""))'
}

wait_observation_owner() {
  local expected="$1"
  local deadline=$((SECONDS + TIMEOUT_SECONDS)) current
  while (( SECONDS < deadline )); do
    current="$(observation_owner 2>/dev/null || true)"
    [[ "$current" == "$expected" ]] && return 0
    sleep 1
  done
  echo "timeout waiting for observation owner $expected; current=$(observation_owner 2>/dev/null || true)" >&2
  return 1
}

claim_workload_lease() {
  local owner="$1" ttl="$2" response
  response="$(curl -fsS -X POST "$BASE_URL/api/v1/agent/reconcile-lease/claim" \
    -H 'Content-Type: application/json' \
    -d "{\"clusterId\":\"$CLUSTER_ID\",\"kind\":\"Workload\",\"resourceId\":\"$WORKLOAD_ID\",\"owner\":\"$owner\",\"ttlSeconds\":$ttl}")"
  RESPONSE="$response" python - <<'PY'
import json, os
data=json.loads(os.environ["RESPONSE"])
assert data.get("claimed") is True, data
PY
}

release_workload_lease() {
  local owner="$1"
  curl -fsS -X POST "$BASE_URL/api/v1/agent/reconcile-lease/release" \
    -H 'Content-Type: application/json' \
    -d "{\"clusterId\":\"$CLUSTER_ID\",\"kind\":\"Workload\",\"resourceId\":\"$WORKLOAD_ID\",\"owner\":\"$owner\"}" \
    >/dev/null
}

wait_workload_lease_free() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS)) response claimed
  while (( SECONDS < deadline )); do
    response="$(curl -fsS -X POST "$BASE_URL/api/v1/agent/reconcile-lease/claim" \
      -H 'Content-Type: application/json' \
      -d "{\"clusterId\":\"$CLUSTER_ID\",\"kind\":\"Workload\",\"resourceId\":\"$WORKLOAD_ID\",\"owner\":\"golden-lease-probe\",\"ttlSeconds\":5}")"
    claimed="$(RESPONSE="$response" python - <<'PY'
import json, os
print("true" if json.loads(os.environ["RESPONSE"]).get("claimed") is True else "false")
PY
)"
    if [[ "$claimed" == "true" ]]; then
      release_workload_lease "golden-lease-probe"
      return 0
    fi
    sleep 1
  done
  echo "timeout waiting for original workload lease release" >&2
  return 1
}

kind get clusters | grep -qx "$CLUSTER_ID" || kind create cluster --name "$CLUSTER_ID" --image kindest/node:v1.34.0 --wait 120s
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"
kubectl wait --for=condition=Available deployment/kueue-controller-manager -n kueue-system --timeout="${TIMEOUT_SECONDS}s"

# Controller Deployment=Available does not prove that the Kueue admission
# webhook is reachable through the Kubernetes Service yet. A real, server-side
# dry-run CREATE of the resource kind installed by Fake GPU (Deployment)
# exercises API-server -> admission-webhook connectivity without creating Pods.
# Do not skip admission or relax webhook failurePolicy to make CI green.
wait_kueue_admission() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS)) attempt=0
  local error_file
  error_file="$(mktemp)"
  while (( SECONDS < deadline )); do
    attempt=$((attempt + 1))
    if kubectl create --dry-run=server -n default -f - >/dev/null 2>"$error_file" <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kueue-admission-readiness-probe
spec:
  replicas: 0
  selector:
    matchLabels:
      app: kueue-admission-readiness-probe
  template:
    metadata:
      labels:
        app: kueue-admission-readiness-probe
    spec:
      containers:
        - name: probe
          image: busybox:1.36
          command: ["true"]
YAML
    then
      rm -f "$error_file"
      echo "Kueue Deployment admission webhook accepted server-side dry-run (attempt=$attempt)"
      return 0
    fi
    sleep 3
  done
  echo "Kueue admission webhook not ready within ${TIMEOUT_SECONDS}s (attempts=$attempt)" >&2
  cat "$error_file" >&2
  rm -f "$error_file"
  kubectl get endpoints,endpointslices -n kueue-system -o wide >&2 || true
  kubectl get pods -n kueue-system -o wide >&2 || true
  return 1
}
wait_kueue_admission

node="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl label node "$node" topology.kubernetes.io/zone=gpu-zone-a ai.compute/rack=rack-a01 "ai.compute/accelerator-class=$ACCELERATOR_CLASS" --overwrite
bash scripts/e2e/install-fake-gpu.sh
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

go run ./cmd/control-plane >/tmp/go-control-plane.log 2>&1 &
CONTROL_PID=$!
wait_http

go build -o /tmp/gpu-cluster-agent ./cmd/cluster-agent

# The full-stack OIDC fixture pre-seeds bindings in the shared PostgreSQL
# database. Verify the authoritative rows rather than replaying a generation-1
# CAS write. Standalone Golden Path runs continue to create their own bindings.
if [[ "${PRESEEDED_BINDINGS:-0}" == "1" ]]; then
  : "${GPU_MANAGEMENT_E2E_PG_CONTAINER:?shared PostgreSQL fixture is required}"
  project_binding="$(docker exec "$GPU_MANAGEMENT_E2E_PG_CONTAINER" psql -X -qAt -U gpu -d gpu_management_fullstack_ci \
    -v "project_id=$PROJECT_ID" -c "SELECT project_id || '|' || cluster_id || '|' || namespace FROM project_bindings WHERE project_id=:'project_id'")"
  expected_project_binding="${PROJECT_ID}|${CLUSTER_ID}|${NAMESPACE}"
  [[ "$project_binding" == "$expected_project_binding" ]] || {
    echo "preseeded project binding mismatch: expected=$expected_project_binding actual=$project_binding" >&2
    exit 1
  }
  pool_binding="$(docker exec "$GPU_MANAGEMENT_E2E_PG_CONTAINER" psql -X -qAt -U gpu -d gpu_management_fullstack_ci \
    -v "pool_id=$POOL_ID" -c "SELECT pool_id || '|' || cluster_id || '|' || provider FROM cluster_bindings WHERE pool_id=:'pool_id'")"
  expected_pool_binding="${POOL_ID}|${CLUSTER_ID}|kueue"
  [[ "$pool_binding" == "$expected_pool_binding" ]] || {
    echo "preseeded Kueue binding mismatch: expected=$expected_pool_binding actual=$pool_binding" >&2
    exit 1
  }
  echo "PASS: verified preseeded project and sole Kueue provider bindings"
else
  put "/api/v1/projects/$PROJECT_ID/binding" "{\"metadata\":{\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"clusterId\":\"$CLUSTER_ID\",\"namespace\":\"$NAMESPACE\"}"
  put "/api/v1/compute-pools/$POOL_ID/binding" "{\"metadata\":{\"generation\":1},\"poolId\":\"$POOL_ID\",\"clusterId\":\"$CLUSTER_ID\",\"provider\":\"kueue\"}"
fi
put "/api/v1/compute-pools/$POOL_ID" "{\"metadata\":{\"id\":\"$POOL_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"spec\":{\"accelerators\":[{\"class\":\"$ACCELERATOR_CLASS\",\"quota\":4}],\"acceleratorBindings\":[{\"class\":\"$ACCELERATOR_CLASS\",\"resourceName\":\"$ACCELERATOR_RESOURCE\",\"flavor\":\"$ACCELERATOR_FLAVOR\",\"nodeLabels\":{\"nvidia.com/gpu.product\":\"NVIDIA-H100-80GB-HBM3\",\"topology.kubernetes.io/zone\":\"gpu-zone-a\",\"ai.compute/rack\":\"rack-a01\"}}],\"scheduling\":{\"mode\":\"default\"}}}"
put "/api/v1/workloads/$WORKLOAD_ID" "{\"metadata\":{\"id\":\"$WORKLOAD_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"echo go-kind-kueue-golden && sleep 5\"],\"accelerator\":{\"class\":\"$ACCELERATOR_CLASS\",\"quota\":1}}}"

AGENT_INSTANCE_ID=agent-a AGENT_SYNC_INTERVAL=60s \
  CLUSTER_ID="$CLUSTER_ID" CONTROL_PLANE_URL="$BASE_URL" \
  /tmp/gpu-cluster-agent >/tmp/go-cluster-agent-a.log 2>&1 &
AGENT_A_PID=$!

capability_status="$(wait_cluster_capabilities)"
CAPABILITY_STATUS="$capability_status" ACCELERATOR_CLASS="$ACCELERATOR_CLASS" python - <<'PY'
import json, os
data=json.loads(os.environ["CAPABILITY_STATUS"])
caps=data["capabilities"]
assert caps["kueue"] is True, caps
assert caps.get("providers") == ["kueue"], caps
assert os.environ["ACCELERATOR_CLASS"] in caps.get("accelerators", []), caps
PY

result="$(wait_workload)"
condition_true "$result" "Admitted"

RESULT="$result" CLUSTER_ID="$CLUSTER_ID" NAMESPACE="$NAMESPACE" WORKLOAD_ID="$WORKLOAD_ID" python - <<'PY'
import json, os
data=json.loads(os.environ["RESULT"])
evidence=((data.get("status") or {}).get("evidenceRefs") or [])
job=f'k8s://{os.environ["CLUSTER_ID"]}/namespaces/{os.environ["NAMESPACE"]}/jobs/job-{os.environ["WORKLOAD_ID"]}'
assert job in evidence, evidence
assert any(ref.startswith(f'kueue://{os.environ["CLUSTER_ID"]}/namespaces/{os.environ["NAMESPACE"]}/workloads/') for ref in evidence), evidence
PY

requested="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].resources.requests.nvidia\.com/gpu}')"
[[ "$requested" == "1" ]] || { echo "expected $ACCELERATOR_RESOURCE request=1, got $requested" >&2; exit 1; }
generation="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.ai\.compute/generation}')"
[[ "$generation" == "1" ]] || { echo "expected workload generation annotation=1, got $generation" >&2; exit 1; }
flavor_label="$(kubectl get resourceflavor "$ACCELERATOR_FLAVOR" -o jsonpath='{.spec.nodeLabels.nvidia\.com/gpu\.product}')"
[[ "$flavor_label" == "NVIDIA-H100-80GB-HBM3" ]] || { echo "unexpected H100 flavor node label: $flavor_label" >&2; exit 1; }
flavor_zone="$(kubectl get resourceflavor "$ACCELERATOR_FLAVOR" -o json | python -c 'import json,sys; print(json.load(sys.stdin)["spec"]["nodeLabels"].get("topology.kubernetes.io/zone", ""))')"
flavor_rack="$(kubectl get resourceflavor "$ACCELERATOR_FLAVOR" -o json | python -c 'import json,sys; print(json.load(sys.stdin)["spec"]["nodeLabels"].get("ai.compute/rack", ""))')"
[[ "$flavor_zone" == "gpu-zone-a" ]] || { echo "unexpected H100 flavor zone: $flavor_zone" >&2; exit 1; }
[[ "$flavor_rack" == "rack-a01" ]] || { echo "unexpected H100 flavor rack: $flavor_rack" >&2; exit 1; }
scheduled_node="$(kubectl get pod -n "$NAMESPACE" -l "ai.compute/workload=job-$WORKLOAD_ID" -o jsonpath='{.items[0].spec.nodeName}')"
[[ "$scheduled_node" == "$node" ]] || { echo "workload escaped topology-aware H100 flavor: $scheduled_node" >&2; exit 1; }

# Process-level HA proof. Agent A is kept alive but idle after its initial
# reconciliation. A short explicit lease then fences Agent B until expiry.
# Killing A must not erase the lease; after expiry B takes ownership and
# reconciles the same immutable Kubernetes Job.
before_uid="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')"
wait_observation_owner "agent-a"
# wait_workload observes the report, but Runner releases its lease just after
# reporting. Synchronize on actual lease availability before installing the
# short acceptance lease so A cannot delete a freshly renewed lease.
wait_workload_lease_free
claim_workload_lease "agent-a" 12

AGENT_INSTANCE_ID=agent-b AGENT_SYNC_INTERVAL=1s \
  CLUSTER_ID="$CLUSTER_ID" CONTROL_PLANE_URL="$BASE_URL" \
  /tmp/gpu-cluster-agent >/tmp/go-cluster-agent-b.log 2>&1 &
AGENT_B_PID=$!

sleep 3
owner_before_expiry="$(observation_owner)"
[[ "$owner_before_expiry" == "agent-a" ]] || {
  echo "agent B wrote before A lease expired: owner=$owner_before_expiry" >&2
  exit 1
}

kill "$AGENT_A_PID"
wait "$AGENT_A_PID" 2>/dev/null || true
AGENT_A_PID=""

# Process death must not implicitly release the durable resource lease.
sleep 3
owner_after_death="$(observation_owner)"
[[ "$owner_after_death" == "agent-a" ]] || {
  echo "A lease disappeared before expiry after process death: owner=$owner_after_death" >&2
  exit 1
}

wait_observation_owner "agent-b"
# Writer identity alone is insufficient: ReconcileFailed observations also
# carry the current owner. Require the complete healthy Golden Path state from B.
takeover_result="$(wait_workload)"
condition_true "$takeover_result" "Admitted"
owner_after_takeover="$(observation_owner)"
[[ "$owner_after_takeover" == "agent-b" ]] || {
  echo "healthy takeover observation was not written by agent-b: owner=$owner_after_takeover" >&2
  exit 1
}
after_uid="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')"
[[ "$before_uid" == "$after_uid" ]] || {
  echo "lease takeover replaced workload Job: before=$before_uid after=$after_uid" >&2
  exit 1
}

# Replaying desired state must not drift the resolved Kubernetes projection.
before="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o json)"
put "/api/v1/workloads/$WORKLOAD_ID" "{\"metadata\":{\"id\":\"$WORKLOAD_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"echo go-kind-kueue-golden && sleep 5\"],\"accelerator\":{\"class\":\"$ACCELERATOR_CLASS\",\"quota\":1}}}"
sleep 3
after="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o json)"
BEFORE="$before" AFTER="$after" python - <<'PY'
import json, os
before=json.loads(os.environ["BEFORE"])
after=json.loads(os.environ["AFTER"])
def projection(obj):
    c=obj["spec"]["template"]["spec"]["containers"][0]
    return {
        "requests": c["resources"]["requests"],
        "limits": c["resources"]["limits"],
        "annotations": obj["metadata"].get("annotations", {}),
        "labels": obj["metadata"].get("labels", {}),
    }
assert projection(before) == projection(after), (projection(before), projection(after))
PY

# Active workload execution intent is immutable. A changed generation/spec must
# use Delete -> provider cleanup -> Finalize -> Recreate rather than hot replacement.
replacement_code="$(curl -sS -o /tmp/workload-replacement.out -w '%{http_code}' -X PUT   "$BASE_URL/api/v1/workloads/$WORKLOAD_ID"   -H 'Content-Type: application/json'   -d "{\"metadata\":{\"id\":\"$WORKLOAD_ID\",\"generation\":2},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.37\",\"command\":[\"sh\",\"-c\",\"echo replacement\"],\"accelerator\":{\"class\":\"$ACCELERATOR_CLASS\",\"quota\":1}}}")"
[[ "$replacement_code" == "409" ]] || { echo "active workload replacement returned HTTP $replacement_code" >&2; cat /tmp/workload-replacement.out >&2; exit 1; }
generation="$(kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.ai\.compute/generation}')"
[[ "$generation" == "1" ]] || { echo "rejected replacement changed provider generation to $generation" >&2; exit 1; }

# An unbound portable class must be rejected at the control-plane admission
# boundary. Invalid intent must never enter desired state or reach Kubernetes.
invalid_code="$(curl -sS -o /tmp/invalid-workload.out -w '%{http_code}' -X PUT \
  "$BASE_URL/api/v1/workloads/$INVALID_WORKLOAD_ID" \
  -H 'Content-Type: application/json' \
  -d "{\"metadata\":{\"id\":\"$INVALID_WORKLOAD_ID\",\"generation\":1},\"projectId\":\"$PROJECT_ID\",\"poolId\":\"$POOL_ID\",\"spec\":{\"image\":\"busybox:1.36\",\"command\":[\"sh\",\"-c\",\"exit 0\"],\"accelerator\":{\"class\":\"a100-invalid\",\"quota\":1}}}")"
[[ "$invalid_code" == "409" ]] || {
  echo "unbound accelerator admission returned HTTP $invalid_code" >&2
  cat /tmp/invalid-workload.out >&2
  exit 1
}
invalid_read_code="$(curl -sS -o /dev/null -w '%{http_code}' \
  "$BASE_URL/api/v1/workloads/$INVALID_WORKLOAD_ID" || true)"
[[ "$invalid_read_code" == "404" ]] || {
  echo "rejected workload leaked into desired state: HTTP $invalid_read_code" >&2
  exit 1
}
if kubectl get job "job-$INVALID_WORKLOAD_ID" -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "fail-closed violated: rejected accelerator workload created a Job" >&2
  exit 1
fi

delete_desired() {
  local kind="$1" resource_id="$2"
  code="$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE \
    "$BASE_URL/api/v1/internal/clusters/$CLUSTER_ID/desired/$kind/$resource_id")"
  [[ "$code" == "202" ]] || { echo "delete $kind/$resource_id returned HTTP $code" >&2; exit 1; }
}

wait_api_gone() {
  local path="$1"
  local deadline=$((SECONDS + TIMEOUT_SECONDS)) code
  while (( SECONDS < deadline )); do
    code="$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL$path" || true)"
    [[ "$code" == "404" ]] && return 0
    sleep 2
  done
  echo "timeout waiting for API resource to disappear: $path" >&2
  return 1
}

# Optional combined Management API ACK-drop experiment. Run it while the
# Control Plane, Cluster Agent, Kueue and parent ComputePool remain live.
if [[ "${RUN_MANAGEMENT_LOST_ACK_ACCEPTANCE:-0}" == "1" ]]; then
  : "${ACK_OIDC_ISSUER:?disposable OIDC issuer is required for lost-ACK acceptance}"
  : "${ACK_OIDC_CA_CERT:?disposable OIDC CA certificate is required for lost-ACK acceptance}"
  : "${IMAGE:?digest-pinned workload image is required for lost-ACK acceptance}"
  # Mint a fresh short-lived token after cluster startup to avoid expiration.
  token_response="$(curl --silent --show-error --fail --max-time 20 \
    --cacert "$ACK_OIDC_CA_CERT" \
    -X POST "$ACK_OIDC_ISSUER/protocol/openid-connect/token" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    --data-urlencode 'grant_type=password' \
    --data-urlencode 'client_id=gpu-e2e-direct' \
    --data-urlencode 'client_secret=e2e-direct-secret-not-for-production' \
    --data-urlencode 'username=ci-operator' \
    --data-urlencode 'password=ci-disposable-password-not-for-production')"
  export OIDC_ACCESS_TOKEN="$(TOKEN_RESPONSE="$token_response" python3 -c 'import json,os,sys; token=json.loads(os.environ["TOKEN_RESPONSE"]).get("access_token",""); sys.stdout.write(token) if token else sys.exit("token response omitted access_token")')"
  unset token_response
  if [[ -z "$OIDC_ACCESS_TOKEN" ]]; then
    echo "FAIL: disposable OIDC provider did not issue an access token" >&2
    exit 1
  fi
  echo "Running Management lost-ACK acceptance on the shared disposable PostgreSQL + kind/Kueue stack"
  ( unset WORKLOAD_ID; make management-compute-lost-ack-acceptance )
  unset OIDC_ACCESS_TOKEN
fi

# A realized workload must be cleaned at the provider before desired state is
# finalized. The finalization transaction also removes observation and lease.
delete_desired "Workload" "$WORKLOAD_ID"
wait_api_gone "/api/v1/workloads/$WORKLOAD_ID"
if kubectl get job "job-$WORKLOAD_ID" -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "workload desired state finalized before provider Job was gone" >&2
  exit 1
fi

# Pool cleanup is dependency-gated behind workloads and removes resources
# owned by that pool. ResourceFlavor is cluster-scoped and may be shared by
# multiple pools, so pool finalization must not garbage-collect it without
# explicit control-plane ownership/reference tracking.
delete_desired "ComputePool" "$POOL_ID"
wait_api_gone "/api/v1/compute-pools/$POOL_ID"
if kubectl get localqueue "lq-$POOL_ID" -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "LocalQueue survived pool finalization" >&2
  exit 1
fi
if kubectl get clusterqueue "cq-$POOL_ID" >/dev/null 2>&1; then
  echo "ClusterQueue survived pool finalization" >&2
  exit 1
fi
if ! kubectl get resourceflavor "$ACCELERATOR_FLAVOR" >/dev/null 2>&1; then
  echo "shared ResourceFlavor was incorrectly deleted by pool finalization" >&2
  exit 1
fi

echo "$result"
echo "GO GOLDEN PASS: Control Plane -> Capability Registration -> Agent A/B lease fence -> kind -> Kueue -> Job/Pod -> Observation -> Finalizer/Delete"
