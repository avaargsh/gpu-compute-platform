#!/usr/bin/env bash
# Real Chromium -> Vite same-origin proxy -> Go Management API -> PostgreSQL.
# Browser IdP issuance and Go introspection provider are TEST DOUBLES.
# Hardened to run only with an explicit TEST_POSTGRES_DSN ephemeral CI DB.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
: "${TEST_POSTGRES_DSN:?TEST_POSTGRES_DSN required for test-only browser+PostgreSQL E2E}"
export MANAGEMENT_E2E_TEST_ONLY=1
export VITE_MANAGEMENT_OIDC_ISSUER=https://idp.example.test/realms/gpu
export VITE_MANAGEMENT_OIDC_CLIENT_ID=gpu-management-public

TMP_DIR="$(mktemp -d)"
API_PID=""
VITE_PID=""
cleanup() {
  trap - EXIT INT TERM
  if [[ -n "$VITE_PID" ]]; then kill "$VITE_PID" 2>/dev/null || true; wait "$VITE_PID" 2>/dev/null || true; fi
  if [[ -n "$API_PID" ]]; then kill "$API_PID" 2>/dev/null || true; wait "$API_PID" 2>/dev/null || true; fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

go build -o "$TMP_DIR/management-e2e-fixture" ./cmd/management-e2e-fixture
"$TMP_DIR/management-e2e-fixture" >"$TMP_DIR/go.log" 2>&1 &
API_PID=$!
for attempt in $(seq 1 60); do
  if curl -fsS --max-time 1 http://127.0.0.1:8081/healthz >/dev/null 2>&1; then break; fi
  if ! kill -0 "$API_PID" 2>/dev/null; then cat "$TMP_DIR/go.log" >&2; exit 1; fi
  if [[ "$attempt" == 60 ]]; then cat "$TMP_DIR/go.log" >&2; exit 1; fi
  sleep 1
done

(
  cd frontend
  exec env -u OIDC_CLIENT_SECRET -u MANAGEMENT_DATABASE_URL -u MANAGEMENT_ACCESS_TOKEN \
    node node_modules/vite/bin/vite.js --config vite.management.config.ts --host 127.0.0.1 >"$TMP_DIR/vite.log" 2>&1
) &
VITE_PID=$!
for attempt in $(seq 1 45); do
  if curl -fsS --max-time 1 http://127.0.0.1:3002/management.html >/dev/null 2>&1; then break; fi
  if ! kill -0 "$VITE_PID" 2>/dev/null; then cat "$TMP_DIR/vite.log" >&2; exit 1; fi
  if [[ "$attempt" == 45 ]]; then cat "$TMP_DIR/vite.log" >&2; exit 1; fi
  sleep 1
done

if ! (cd frontend && npx playwright test --config playwright.management.live.config.ts); then
  echo "Go Management fixture and Vite logs (no tokens included):" >&2
  tail -n 60 "$TMP_DIR/go.log" >&2
  tail -n 40 "$TMP_DIR/vite.log" >&2
  exit 1
fi
