#!/usr/bin/env bash
# Test-only STATIC HTTPS same-origin Caddy + real Keycloak PKCE + scoped Go + TLS PG.
# Every dependency is disposable and loopback-bound. No production credentials.
set -Eeuo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d)"
chmod 755 "$TMP"
PG="gpu-management-browser-pg-$$"
KC="gpu-management-browser-kc-$$"
API_PID=""
GW="gpu-management-static-tls-$BASHPID"
cleanup() {
  trap - EXIT INT TERM
  for id in "$API_PID"; do
    if [[ -n "$id" ]]; then kill "$id" >/dev/null 2>&1 || true; wait "$id" >/dev/null 2>&1 || true; fi
  done
  docker rm -f "$GW" "$KC" "$PG" >/dev/null 2>&1 || true
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
  -v "$ROOT/scripts/e2e/keycloak-management-browser-tls-realm.json:/opt/keycloak/data/import/realm.json:ro" \
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
export MANAGEMENT_BROWSER_ORIGIN=https://127.0.0.1:3443
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

# Build the REAL production-like immutable static Vite assets with public
# OIDC client config. Never place OIDC client secret, DB URL or credentials
# inside this frontend bundle.
export VITE_MANAGEMENT_OIDC_ISSUER="$ISSUER"
export VITE_MANAGEMENT_OIDC_CLIENT_ID=gpu-management-browser-e2e
(cd frontend && env -u MANAGEMENT_DATABASE_URL -u OIDC_CLIENT_SECRET -u OIDC_INTROSPECTION_URL \
  npm run management:build)

openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout "$TMP/gateway.key" -out "$TMP/gateway.crt" \
  -subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1" >/dev/null 2>&1
chmod 600 "$TMP/gateway.key"
chmod 644 "$TMP/gateway.crt"
# Host networking is ONLY for disposable CI: both backends are restricted to
# numeric 127.0.0.1, which is invisible to ordinary remote clients. An
# external Kubernetes ingress is intentionally not created.
docker run -d --name "$GW" --network host --read-only \
  --user "$(id -u):$(id -g)" --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,nosuid,nodev,mode=1777 \
  -e XDG_DATA_HOME=/tmp/caddy -e XDG_CONFIG_HOME=/tmp/caddy \
  -v "$ROOT/frontend/dist:/srv/management:ro" \
  -v "$ROOT/deploy/local/management/Caddyfile:/etc/caddy/Caddyfile:ro" \
  -v "$TMP/gateway.crt:/etc/management-tls/server.crt:ro" \
  -v "$TMP/gateway.key:/etc/management-tls/server.key:ro" \
  caddy:2.11.6-alpine caddy run --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
ready=0
for attempt in $(seq 1 90); do
  if curl -fsS --max-time 2 --cacert "$TMP/gateway.crt" \
      https://127.0.0.1:3443/management.html >/dev/null 2>&1; then
    ready=1; break
  fi
  if ! docker inspect -f '{{.State.Running}}' "$GW" 2>/dev/null | grep -qx true; then
    docker logs "$GW" >&2 || true
    echo "FAIL isolated static HTTPS gateway did not start" >&2
    exit 1
  fi
  sleep 0.2
done
if [[ "$ready" != 1 ]]; then
  docker logs --tail 100 "$GW" >&2 || true
  echo "FAIL verified HTTPS static page not ready" >&2
  exit 1
fi

# Negative ingress matrix: reject any Agent/internal API path; no API bearer
# gives 401 (never unprotected fallback); unrelated URL returns 404.
for path in /api/v1/agent/register /api/v1/internal/config /api/v1/projects /; do
  status="$(curl -s -o /dev/null -w '%{http_code}' --cacert "$TMP/gateway.crt" \
      "https://127.0.0.1:3443$path")"
  [[ "$status" == 404 ]] || { echo "FAIL disallowed gateway path: $path returned $status" >&2; exit 1; }
done
status="$(curl -s -o /dev/null -w '%{http_code}' --cacert "$TMP/gateway.crt" \
    https://127.0.0.1:3443/api/v1/management/projects)"
[[ "$status" == 401 ]] || { echo "FAIL anonymous Management API gateway access ($status)" >&2; exit 1; }
curl -fsSI --cacert "$TMP/gateway.crt" https://127.0.0.1:3443/management.html > "$TMP/static-headers"
grep -qi '^x-content-type-options: nosniff' "$TMP/static-headers" || {
  echo "FAIL gateway missing nosniff" >&2; exit 1;
}
grep -qi '^cache-control: no-store' "$TMP/static-headers" || {
  echo "FAIL gateway permitted caching" >&2; exit 1;
}
grep -qi '^content-security-policy:' "$TMP/static-headers" || {
  echo "FAIL gateway missing CSP" >&2; exit 1;
}
# The browser test performs real Keycloak HTTPS login, real Management
# introspection, real signed Postgres TLS, a Desired + immutable audit
# receipt, and same-token grant downgrade + revocation without mock routes.
export GPU_MANAGEMENT_E2E_PG_CONTAINER="$PG"
(cd frontend && npx playwright test --config playwright.management.static-tls.config.ts)
echo "PASS real static HTTPS ingress + Keycloak PKCE + scoped PG TLS CAS/audit/revocation"
