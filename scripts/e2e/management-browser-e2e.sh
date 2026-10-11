#!/usr/bin/env bash
# Real Chromium and real Vite Management UI; provider and management HTTP
# endpoints are simulated in Playwright. Never claim real IdP or DB proof.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
TMP_LOG="$(mktemp)"
VITE_PID=""
cleanup() {
  trap - EXIT INT TERM
  if [[ -n "$VITE_PID" ]]; then
    kill "$VITE_PID" 2>/dev/null || true
    wait "$VITE_PID" 2>/dev/null || true
  fi
  rm -f "$TMP_LOG"
}
trap cleanup EXIT
export VITE_MANAGEMENT_OIDC_ISSUER=https://idp.example.test/realms/gpu
export VITE_MANAGEMENT_OIDC_CLIENT_ID=gpu-management-public

(
  cd frontend
  exec env -u MANAGEMENT_DATABASE_URL -u OIDC_CLIENT_SECRET -u MANAGEMENT_ACCESS_TOKEN \
    node node_modules/vite/bin/vite.js --config vite.management.config.ts --host 127.0.0.1 >"$TMP_LOG" 2>&1
) &
VITE_PID=$!
for attempt in $(seq 1 45); do
  if curl -fsS --max-time 1 http://127.0.0.1:3002/management.html >/dev/null 2>&1; then break; fi
  if ! kill -0 "$VITE_PID" 2>/dev/null; then cat "$TMP_LOG" >&2; exit 1; fi
  if [[ "$attempt" == 45 ]]; then cat "$TMP_LOG" >&2; exit 1; fi
  sleep 1
done

(cd frontend && npx playwright test --config playwright.management.config.ts)
