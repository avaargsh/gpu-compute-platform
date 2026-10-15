#!/usr/bin/env bash
# Disposable real-process smoke for the one-terminal local Portal launcher.
# Never prints/records the operator secret; never writes a production database.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
LOG="$(mktemp)"
LAUNCH_PID=""
cleanup() {
  trap - EXIT INT TERM
  if [[ -n "$LAUNCH_PID" ]]; then
    kill "$LAUNCH_PID" 2>/dev/null || true
    wait "$LAUNCH_PID" 2>/dev/null || true
  fi
  rm -f "$LOG"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

export PORTAL_OPERATOR_CLUSTER_ID=portal-launch-ci
export PORTAL_OPERATOR_PROJECT_IDS=research
export PORTAL_OPERATOR_TOKEN="$(openssl rand -hex 32)"
unset DATABASE_URL

bash scripts/dev/portal-local.sh >"$LOG" 2>&1 &
LAUNCH_PID=$!
for attempt in $(seq 1 120); do
  if ! kill -0 "$LAUNCH_PID" 2>/dev/null; then
    cat "$LOG" >&2
    echo 'local Portal launcher exited before readiness' >&2
    exit 1
  fi
  if curl --silent --fail --max-time 1 http://127.0.0.1:3001/portal.html >/dev/null 2>&1 &&
     curl --silent --fail --max-time 1 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    break
  fi
  if [[ "$attempt" == 120 ]]; then
    cat "$LOG" >&2
    echo 'local Portal launcher did not become ready' >&2
    exit 1
  fi
  sleep 1
done

if grep -Fq -- "$PORTAL_OPERATOR_TOKEN" "$LOG"; then
  echo 'operator secret appeared in noninteractive stdout/stderr' >&2
  exit 1
fi

code() {
  curl -sS -m 6 -o /dev/null -w '%{http_code}' "$@"
}
url=http://127.0.0.1:8080/api/v1/operator/clusters/portal-launch-ci/snapshot
[[ "$(code "$url")" == 401 ]] || { echo 'missing operator credential not denied' >&2; exit 1; }
[[ "$(code -H "Authorization: Bearer $PORTAL_OPERATOR_TOKEN" "$url")" == 404 ]] ||
  { echo 'unregistered cluster should not fake success' >&2; exit 1; }

# Browser origin cannot route a legacy unauthenticated Agent write into Go.
code -X POST -H 'Content-Type: application/json' --data-binary \
  '{"clusterId":"smoke-injected","agentVersion":"fake"}' \
  http://127.0.0.1:3001/api/v1/agent/register >/dev/null
foreign=http://127.0.0.1:8080/api/v1/clusters/smoke-injected/status
[[ "$(code "$foreign")" == 404 ]] ||
  { echo 'Vite forwarded unauthorized Agent write to Go' >&2; exit 1; }

kill "$LAUNCH_PID" 2>/dev/null || true
wait "$LAUNCH_PID" 2>/dev/null || true
LAUNCH_PID=""
for attempt in $(seq 1 30); do
  if ! (echo >/dev/tcp/127.0.0.1/8080) 2>/dev/null &&
     ! (echo >/dev/tcp/127.0.0.1/3001) 2>/dev/null; then
    echo 'portal-local smoke PASS: health, 401, 404, secret redaction, blocked Agent proxy, cleanup'
    exit 0
  fi
  sleep 1
done
echo 'portal-local leaked a listening process after stop' >&2
exit 1
