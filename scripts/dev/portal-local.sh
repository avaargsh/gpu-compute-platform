#!/usr/bin/env bash
# Loopback-only, one-terminal local Portal pilot. NEVER publish on a network.
# No fixture data is created; only registered Agent resources are displayed.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

die() { printf 'portal-local: %s\n' "$*" >&2; exit 1; }
for command in go node npm openssl curl; do
  command -v "$command" >/dev/null 2>&1 || die "missing required command: $command"
done

CLUSTER="${PORTAL_OPERATOR_CLUSTER_ID:-}"
PROJECTS="${PORTAL_OPERATOR_PROJECT_IDS:-}"
[[ -n "$CLUSTER" ]] || die "set PORTAL_OPERATOR_CLUSTER_ID to an already-registered cluster"
[[ -n "$PROJECTS" ]] || die "set PORTAL_OPERATOR_PROJECT_IDS to explicit comma-separated project IDs"
[[ "$PROJECTS" != ,* && "$PROJECTS" != *, && "$PROJECTS" != *,,* ]] || die "invalid empty project ID in comma-separated scope"
[[ "$CLUSTER" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$ ]] || die "invalid cluster ID"
IFS=',' read -ra SCOPES <<< "$PROJECTS"
for project in "${SCOPES[@]}"; do
  [[ "$project" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$ ]] || die "invalid project ID in scope"
done

NEW_TOKEN=0
if [[ -z "${PORTAL_OPERATOR_TOKEN:-}" ]]; then
  [[ -t 1 ]] || die "in noninteractive mode, set PORTAL_OPERATOR_TOKEN explicitly (never print a token into CI logs)"
  export PORTAL_OPERATOR_TOKEN="$(openssl rand -hex 32)"
  NEW_TOKEN=1
fi
[[ "${#PORTAL_OPERATOR_TOKEN}" -ge 32 ]] || die "PORTAL_OPERATOR_TOKEN must be at least 32 characters"
export PORTAL_OPERATOR_CLUSTER_ID="$CLUSTER"
export PORTAL_OPERATOR_PROJECT_IDS="$PROJECTS"
export CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:8080

# A pre-existing unrelated local server must never be mistaken for our API.
for port in 8080 3001; do
  if (echo >/dev/tcp/127.0.0.1/"$port") >/dev/null 2>&1; then
    die "127.0.0.1:$port is already in use; stop the existing process first"
  fi
done

if [[ ! -f frontend/node_modules/vite/bin/vite.js ]]; then
  printf 'portal-local: installing existing locked frontend dependencies\n'
  (cd frontend && npm ci)
fi

WORK_DIR="$(mktemp -d)"
GO_PID=""
VITE_PID=""
cleanup() {
  trap - EXIT INT TERM
  for pid in "$VITE_PID" "$GO_PID"; do
    if [[ -n "$pid" ]]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

go build -o "$WORK_DIR/control-plane" ./cmd/control-plane
"$WORK_DIR/control-plane" >"$WORK_DIR/go.log" 2>&1 &
GO_PID=$!
for attempt in $(seq 1 45); do
  if ! kill -0 "$GO_PID" 2>/dev/null; then
    cat "$WORK_DIR/go.log" >&2
    die "Go control-plane exited before readiness"
  fi
  if curl --silent --fail --max-time 1 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    break
  fi
  if [[ "$attempt" == 45 ]]; then
    cat "$WORK_DIR/go.log" >&2
    die "Go control-plane did not become ready"
  fi
  sleep 1
done

(
  cd frontend
  # Vite does not need operator or database credentials; do not expose
  # them to its plugin/config process.
  exec env -u PORTAL_OPERATOR_TOKEN -u PORTAL_OPERATOR_PROJECT_IDS -u PORTAL_OPERATOR_CLUSTER_ID -u DATABASE_URL node node_modules/vite/bin/vite.js --config vite.portal.config.ts --host 127.0.0.1
) >"$WORK_DIR/vite.log" 2>&1 &
VITE_PID=$!
for attempt in $(seq 1 45); do
  if ! kill -0 "$VITE_PID" 2>/dev/null; then
    cat "$WORK_DIR/vite.log" >&2
    die "Vue Portal exited before readiness"
  fi
  if curl --silent --fail --max-time 1 http://127.0.0.1:3001/portal.html >/dev/null 2>&1; then
    break
  fi
  if [[ "$attempt" == 45 ]]; then
    cat "$WORK_DIR/vite.log" >&2
    die "Vue Portal did not become ready"
  fi
  sleep 1
done

printf '\nGPU Compute Portal — LOCAL, READ-ONLY preview\n'
printf 'URL:       http://127.0.0.1:3001/portal.html\n'
printf 'Cluster:   %s\n' "$CLUSTER"
printf 'Projects:  %s\n' "$PROJECTS"
if [[ "$NEW_TOKEN" -eq 1 ]]; then
  # Only show a newly generated secret to a terminal, never a redirected log.
  printf 'One-time operator token (paste into Portal): %s\n' "$PORTAL_OPERATOR_TOKEN"
else
  printf 'Token:     provided through the current shell environment (not printed)\n'
fi
if [[ -z "${DATABASE_URL:-}" ]]; then
  printf 'Storage:   IN MEMORY — no real resources until an Agent registers; state is lost at shutdown\n'
else
  printf 'Storage:   configured PostgreSQL; state may persist across restarts\n'
fi
printf 'Security:  localhost only; no OIDC / multi-user RBAC; never publish port 8080 or 3001\n'
printf 'Stop:      Ctrl-C (terminates both child processes)\n\n'

# Exiting either service closes both; no orphaned local preview processes.
if wait -n "$GO_PID" "$VITE_PID"; then
  printf 'portal-local: a child exited unexpectedly\n' >&2
else
  printf 'portal-local: a child failed; inspect recent logs below\n' >&2
fi
tail -n 30 "$WORK_DIR/go.log" >&2 || true
tail -n 30 "$WORK_DIR/vite.log" >&2 || true
exit 1
