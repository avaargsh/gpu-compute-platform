#!/usr/bin/env bash
# Test-only real HTTPS Keycloak -> browser PKCE -> scoped Go API -> verified TLS PG.
# Every dependency is disposable and loopback-bound. No production credentials.
set -Eeuo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d)"
chmod 755 "$TMP"
PG="gpu-management-browser-pg-$$"
KC="gpu-management-browser-kc-$$"
API_PID=""
VITE_PID=""
cleanup() {
  trap - EXIT INT TERM
  for id in "$VITE_PID" "$API_PID"; do
    if [[ -n "$id" ]]; then kill "$id" >/dev/null 2>&1 || true; wait "$id" >/dev/null 2>&1 || true; fi
  done
  docker rm -f "$KC" "$PG" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT INT TERM
ISSUER="https://127.0.0.1:8443/realms/gpu-management-e2e"

# Independent real identity-provider TLS endpoint. Self-signed cert is trusted
# ONLY by the disposable Go process; Chromium uses ignoreHTTPSErrors in E2E.
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout "$TMP/keycloak.key" -out "$TMP/keycloak.crt" \
  -subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1" >/dev/null 2>&1
chmod 644 "$TMP/keycloak.key" "$TMP/keycloak.crt"
docker run -d --rm --name "$KC" --memory 1536m \
  -p 127.0.0.1:8443:8443 \
  -e KC_BOOTSTRAP_ADMIN_USERNAME=ci-browser-admin \
  -e KC_BOOTSTRAP_ADMIN_PASSWORD=ci-disposable-browser-only \
  -v "$ROOT/scripts/e2e/keycloak-management-browser-realm.json:/opt/keycloak/data/import/realm.json:ro" \
  -v "$TMP/keycloak.crt:/opt/keycloak/conf/browser.crt:ro" \
  -v "$TMP/keycloak.key:/opt/keycloak/conf/browser.key:ro" \
  quay.io/keycloak/keycloak:26.8.0@sha256:27b3230fbabb8c9ff3c66c864cf09dcd8a41aca82e5100679d109ef0eeb05741 \
  start-dev --import-realm --hostname=https://127.0.0.1:8443 \
  --https-port=8443 --http-enabled=false \
  --https-certificate-file=/opt/keycloak/conf/browser.crt \
  --https-certificate-key-file=/opt/keycloak/conf/browser.key >/dev/null
ready=0
for attempt in $(seq 1 120); do
  if curl -fsS --max-time 2 --cacert "$TMP/keycloak.crt" \
       "$ISSUER/.well-known/openid-configuration" > "$TMP/discovery.json" 2>/dev/null; then
    ready=1; break
  fi
  if ! docker inspect -f '{{.State.Running}}' "$KC" 2>/dev/null | grep -qx true; then
    echo "FAIL disposable Keycloak container exited" >&2; exit 1
  fi
  sleep 2
done
if [[ "$ready" != 1 ]]; then echo "FAIL disposable HTTPS Keycloak not ready" >&2; exit 1; fi

# Separate real PostgreSQL server TLS identity: hostname=localhost, verify-full.
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout "$TMP/db-ca.key" -out "$TMP/db-ca.pem" \
  -subj "/CN=disposable-db-root" -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1
openssl req -new -newkey rsa:2048 -sha256 -nodes \
  -keyout "$TMP/db-server.key" -out "$TMP/db-server.csr" \
  -subj "/CN=localhost" >/dev/null 2>&1
cat > "$TMP/db-server.ext" <<'CERT'
subjectAltName=DNS:localhost
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
CERT
openssl x509 -req -days 1 -sha256 -in "$TMP/db-server.csr" \
  -CA "$TMP/db-ca.pem" -CAkey "$TMP/db-ca.key" -CAcreateserial \
  -out "$TMP/db-server.crt" -extfile "$TMP/db-server.ext" >/dev/null 2>&1
PG_IMAGE=postgres:17-alpine
PG_UID="$(docker run --rm --entrypoint id "$PG_IMAGE" -u postgres)"
PG_GID="$(docker run --rm --entrypoint id "$PG_IMAGE" -g postgres)"
chmod 600 "$TMP/db-server.key"
sudo chown "$PG_UID:$PG_GID" "$TMP/db-server.key"
chmod 644 "$TMP/db-server.crt" "$TMP/db-ca.pem"
docker run -d --name "$PG" \
  -p 127.0.0.1:55433:5432 \
  -e POSTGRES_USER=gpu -e POSTGRES_PASSWORD=ci-disposable-dba \
  -e POSTGRES_DB=gpu_management_fullstack_ci \
  -v "$TMP/db-server.crt:/tls/server.crt:ro" \
  -v "$TMP/db-server.key:/tls/server.key:ro" \
  "$PG_IMAGE" -c ssl=on -c ssl_cert_file=/tls/server.crt \
  -c ssl_key_file=/tls/server.key -c ssl_min_protocol_version=TLSv1.2 >/dev/null
ready=0
for attempt in $(seq 1 90); do
  if [[ "$(docker exec -e PGPASSWORD=ci-disposable-dba "$PG" psql -X -qAt \
        -h 127.0.0.1 -U gpu -d gpu_management_fullstack_ci -c 'SELECT 1' 2>/dev/null || true)" == 1 ]]; then
    ready=1; break
  fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then echo "FAIL disposable PostgreSQL not ready" >&2; exit 1; fi
psql_fixture() {
  docker exec -i "$PG" psql -X -q -v ON_ERROR_STOP=1 -U gpu -d gpu_management_fullstack_ci "$@"
}
psql_fixture < internal/store/postgres/schema.sql
psql_fixture < internal/platform/management/schema.sql
PASSWORD="$(openssl rand -hex 24)"
psql_fixture <<SQL
CREATE ROLE gpu_management_runtime NOLOGIN;
CREATE ROLE management_fullstack_login LOGIN INHERIT PASSWORD '$PASSWORD';
GRANT gpu_management_runtime TO management_fullstack_login;
INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES
 ('research','browser-ci','research');
INSERT INTO cluster_bindings(pool_id,cluster_id,provider) VALUES
 ('pool-browser-ci','browser-ci','kueue');
INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES
 ('$ISSUER','08d71ae7-c789-4c56-b0b4-6a987efdaa12','research','operator');
SQL
psql_fixture < internal/platform/management/runtime_role.sql

export MANAGEMENT_DATABASE_URL="postgres://management_fullstack_login:$PASSWORD@localhost:55433/gpu_management_fullstack_ci?sslmode=verify-full&sslrootcert=$TMP/db-ca.pem"
export MANAGEMENT_LISTEN_ADDR=127.0.0.1:8081
export MANAGEMENT_BROWSER_ORIGIN=http://127.0.0.1:3002
export OIDC_ISSUER="$ISSUER"
export OIDC_INTROSPECTION_URL="$ISSUER/protocol/openid-connect/token/introspect"
export OIDC_AUDIENCE=gpu-management-api
export OIDC_CLIENT_ID=gpu-e2e-introspection
export OIDC_CLIENT_SECRET=e2e-introspect-secret-not-for-production
export SSL_CERT_FILE="$TMP/keycloak.crt"
go build -o "$TMP/management-api" ./cmd/management-api
"$TMP/management-api" > "$TMP/go.log" 2>&1 &
API_PID=$!
ready=0
for attempt in $(seq 1 100); do
  if curl -fsS --max-time 1 http://127.0.0.1:8081/healthz >/dev/null 2>&1; then
    ready=1; break
  fi
  if ! kill -0 "$API_PID" 2>/dev/null; then
    echo "FAIL Management API rejected its real scoped runtime login; no public listener" >&2
    exit 1
  fi
  sleep 0.15
done
if [[ "$ready" != 1 ]]; then echo "FAIL scoped Go Management API not ready" >&2; exit 1; fi
DB_TLS="$(psql_fixture -tA -c "SELECT count(*) FROM pg_stat_activity a JOIN pg_stat_ssl s USING(pid) WHERE a.usename='management_fullstack_login' AND s.ssl IS TRUE")"
if [[ ! "$DB_TLS" =~ ^[1-9][0-9]*$ ]]; then
  echo "FAIL runtime database connection not TLS" >&2; exit 1
fi

export VITE_MANAGEMENT_OIDC_ISSUER="$ISSUER"
export VITE_MANAGEMENT_OIDC_CLIENT_ID=gpu-management-browser-e2e
(
  cd frontend
  exec env -u MANAGEMENT_DATABASE_URL -u OIDC_CLIENT_SECRET -u OIDC_INTROSPECTION_URL \
    node node_modules/vite/bin/vite.js --config vite.management.config.ts --host 127.0.0.1 \
    > "$TMP/vite.log" 2>&1
) &
VITE_PID=$!
ready=0
for attempt in $(seq 1 60); do
  if curl -fsS --max-time 1 http://127.0.0.1:3002/management.html >/dev/null 2>&1; then
    ready=1; break
  fi
  if ! kill -0 "$VITE_PID" 2>/dev/null; then
    echo "FAIL local Management Vite died" >&2; exit 1
  fi
  sleep 0.15
done
if [[ "$ready" != 1 ]]; then echo "FAIL local Management Vite not ready" >&2; exit 1; fi

export GPU_MANAGEMENT_E2E_PG_CONTAINER="$PG"
(cd frontend && npx playwright test --config playwright.management.fullstack.config.ts)
echo "PASS real Keycloak S256 browser login + scoped PG TLS RBAC/CAS/audit/revocation"

# Opt-in cross-stack test: the Management API keeps its constrained TLS-verified
# login and live Keycloak introspection while Control Plane/Agent share the same
# disposable PostgreSQL desired-state tables and real kind/Kueue execution path.
if [[ "${RUN_MANAGEMENT_LOST_ACK_ACCEPTANCE:-0}" == "1" ]]; then
  export ACK_OIDC_ISSUER="$ISSUER"
  export ACK_OIDC_CA_CERT="$TMP/keycloak.crt"

  export DATABASE_URL="postgres://gpu:ci-disposable-dba@localhost:55433/gpu_management_fullstack_ci?sslmode=verify-full&sslrootcert=$TMP/db-ca.pem"

  # The browser acceptance above deliberately created pool-browser-ci and then
  # revoked the research grant. Use a separate identity scope for this live
  # Control Plane/Kueue experiment so generations and revocation state cannot
  # contaminate the lost-ACK test.
  psql_fixture <<SQL
INSERT INTO project_bindings(project_id,cluster_id,namespace)
VALUES ('lost-ack-project','lost-ack-ci','lost-ack');
INSERT INTO cluster_bindings(pool_id,cluster_id,provider)
VALUES ('pool-lost-ack-ci','lost-ack-ci','kueue');
INSERT INTO management_project_grants(issuer,subject,project_id,role)
VALUES ('$ISSUER','08d71ae7-c789-4c56-b0b4-6a987efdaa12','lost-ack-project','operator');
SQL
  export CLUSTER_ID=lost-ack-ci
  export PROJECT_ID=lost-ack-project
  export POOL_ID=pool-lost-ack-ci
  export NAMESPACE=lost-ack
  export ACCELERATOR_CLASS=h100-80g
  export ACCELERATOR_RESOURCE=nvidia.com/gpu
  export ACCELERATOR_FLAVOR=h100-80g
  export MANAGEMENT_BASE_URL=http://127.0.0.1:8081
  export CONTROL_PLANE_BASE_URL=http://127.0.0.1:8080
  export ACCEPTANCE_TIMEOUT_SECONDS=240

  if ! docker pull busybox:1.36 >"$TMP/busybox-pull.log" 2>&1; then
    cat "$TMP/busybox-pull.log" >&2
    exit 1
  fi
  export IMAGE="$(docker image inspect --format='{{index .RepoDigests 0}}' busybox:1.36)"
  if [[ ! "$IMAGE" =~ @sha256:[0-9a-f]{64}$ ]]; then
    echo "FAIL: could not resolve busybox:1.36 to an immutable image digest" >&2
    exit 1
  fi
  echo "Starting kind/Kueue stack with a shared disposable PostgreSQL database; workload image pinned by digest"
  # project_bindings + cluster_bindings were authoritatively seeded above for
  # Management OIDC and are shared by the Control Plane. The Golden Path script
  # must verify them, not attempt a second generation-1 CAS write.
  export PRESEEDED_BINDINGS=1
  export GPU_MANAGEMENT_E2E_PG_CONTAINER="$PG"
  bash scripts/e2e/go-kind-kueue-golden.sh
fi
