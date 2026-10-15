#!/usr/bin/env bash
# Production-binary TLS transport acceptance on disposable PostgreSQL 17.
# Tests an ACTUAL server-signed TLS connection, not only DSN parsing.
# This script provisions ONLY throwaway CI roles and CA material.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
TMP_DIR="$(mktemp -d)"
chmod 755 "$TMP_DIR"
PG_IMAGE=postgres:17-alpine
PG_CONTAINER="gpu-management-tls-ci-$$"
API_PID=""
cleanup() {
  trap - EXIT INT TERM
  if [[ -n "$API_PID" ]]; then
    kill "$API_PID" >/dev/null 2>&1 || true
    wait "$API_PID" >/dev/null 2>&1 || true
  fi
  docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

# A private disposable CA signs a server cert with a DNS SAN ONLY.
# Connecting through 127.0.0.1 must fail hostname verification even
# though it reaches precisely the same PostgreSQL instance.
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout "$TMP_DIR/ca.key" -out "$TMP_DIR/ca.pem" \
  -subj "/CN=management-postgres-disposable-root" \
  -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1
openssl req -new -newkey rsa:2048 -sha256 -nodes \
  -keyout "$TMP_DIR/server.key" -out "$TMP_DIR/server.csr" \
  -subj "/CN=localhost" >/dev/null 2>&1
cat > "$TMP_DIR/server.ext" <<'CERTEXT'
subjectAltName=DNS:localhost
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
CERTEXT
openssl x509 -req -days 1 -sha256 -in "$TMP_DIR/server.csr" \
  -CA "$TMP_DIR/ca.pem" -CAkey "$TMP_DIR/ca.key" -CAcreateserial \
  -out "$TMP_DIR/server.crt" -extfile "$TMP_DIR/server.ext" >/dev/null 2>&1
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout "$TMP_DIR/untrusted.key" -out "$TMP_DIR/untrusted-ca.pem" \
  -subj "/CN=untrusted-ci-root" \
  -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1

# postgres:17-alpine drops privileges; its key must be owned by the
# actual container PostgreSQL account and readable only by that account.
PG_UID="$(docker run --rm --entrypoint id "$PG_IMAGE" -u postgres)"
PG_GID="$(docker run --rm --entrypoint id "$PG_IMAGE" -g postgres)"
chmod 600 "$TMP_DIR/server.key"
sudo chown "$PG_UID:$PG_GID" "$TMP_DIR/server.key"
chmod 644 "$TMP_DIR/server.crt" "$TMP_DIR/ca.pem" "$TMP_DIR/untrusted-ca.pem"

docker run -d --name "$PG_CONTAINER" \
  -p 127.0.0.1:55433:5432 \
  -e POSTGRES_USER=gpu -e POSTGRES_PASSWORD=ci-only-admin \
  -e POSTGRES_DB=gpu_management_tls_ci \
  -v "$TMP_DIR/server.crt:/tls/server.crt:ro" \
  -v "$TMP_DIR/server.key:/tls/server.key:ro" \
  "$PG_IMAGE" -c ssl=on -c ssl_cert_file=/tls/server.crt \
  -c ssl_key_file=/tls/server.key -c ssl_min_protocol_version=TLSv1.2 >/dev/null

PG_READY=0
for attempt in $(seq 1 90); do
  # pg_isready reports accepting connections even while the named DB
  # has not been created (or while initdb uses a Unix-only temp server).
  # Require a successful SQL read of the EXACT fixture DB over TCP.
  if [[ "$(docker exec -e PGPASSWORD=ci-only-admin "$PG_CONTAINER" \
      psql -X -qAt -h 127.0.0.1 -U gpu -d gpu_management_tls_ci \
      -c 'SELECT 1' 2>/dev/null || true)" == "1" ]]; then
    PG_READY=1
    break
  fi
  if ! docker inspect -f '{{.State.Running}}' "$PG_CONTAINER" 2>/dev/null | grep -qx true; then
    echo "disposable TLS PostgreSQL container stopped unexpectedly" >&2
    exit 1
  fi
  sleep 1
done
if [[ "$PG_READY" != 1 ]]; then
  echo "disposable TLS PostgreSQL did not become ready" >&2
  exit 1
fi

psql_fixture() {
  docker exec -i "$PG_CONTAINER" psql -X -q -v ON_ERROR_STOP=1 \
    -U gpu -d gpu_management_tls_ci "$@"
}
psql_fixture < internal/store/postgres/schema.sql
psql_fixture < internal/platform/management/schema.sql
TEST_PASSWORD="$(openssl rand -hex 20)"
psql_fixture <<SQL
CREATE ROLE gpu_management_runtime NOLOGIN;
CREATE ROLE management_tls_login LOGIN INHERIT PASSWORD '$TEST_PASSWORD';
GRANT gpu_management_runtime TO management_tls_login;
SQL
psql_fixture < internal/platform/management/runtime_role.sql
go build -o "$TMP_DIR/management-api" ./cmd/management-api

export OIDC_ISSUER="https://issuer.example.test/realms/gpu"
export OIDC_INTROSPECTION_URL="https://issuer.example.test/realms/gpu/protocol/openid-connect/token/introspect"
export OIDC_CLIENT_ID=disposable-management-ci
export OIDC_CLIENT_SECRET=ci-only-secret
export OIDC_AUDIENCE=management-api
export MANAGEMENT_LISTEN_ADDR=127.0.0.1:18081

DSN_PREFIX="postgres://management_tls_login:$TEST_PASSWORD@localhost:55433/gpu_management_tls_ci"
VERIFIED_DSN="$DSN_PREFIX?sslmode=verify-full&sslrootcert=$TMP_DIR/ca.pem"

# The same real production process must be able to re-establish a new,
# independently verified TLS session after certificate rotation/rollback.
# Check the HTTPS-independent local health endpoint AND pg_stat_ssl.
expect_verified_login() {
  local case_name="$1" candidate="$2" ready=0
  MANAGEMENT_DATABASE_URL="$candidate" "$TMP_DIR/management-api" > "$TMP_DIR/positive.log" 2>&1 &
  API_PID=$!
  for attempt in $(seq 1 100); do
    if curl -fsS --max-time 1 http://127.0.0.1:18081/healthz >/dev/null 2>&1; then
      ready=1
      break
    fi
    if ! kill -0 "$API_PID" 2>/dev/null; then
      echo "FAIL $case_name: production API did not start with narrow LOGIN" >&2
      exit 1
    fi
    sleep 0.1
  done
  if [[ "$ready" != 1 ]]; then
    echo "FAIL $case_name: loopback API did not become healthy" >&2
    exit 1
  fi
  local ssl_connections
  ssl_connections="$(docker exec "$PG_CONTAINER" psql -X -tA -U gpu -d gpu_management_tls_ci \
    -c "SELECT count(*) FROM pg_stat_activity a JOIN pg_stat_ssl s USING(pid) WHERE a.usename='management_tls_login' AND s.ssl IS TRUE")"
  if [[ ! "$ssl_connections" =~ ^[0-9]+$ ]] || (( ssl_connections < 1 )); then
    echo "FAIL $case_name: management API had no real TLS DB session" >&2
    exit 1
  fi
  kill "$API_PID" >/dev/null 2>&1 || true
  wait "$API_PID" >/dev/null 2>&1 || true
  API_PID=""
  echo "PASS $case_name"
}
expect_verified_login "real verified-TLS startup, least-privilege LOGIN, server pg_stat_ssl" "$VERIFIED_DSN"

# A negative control must fail at the EXPECTED boundary, never bind the API
# and never return a success exit code. Do not print connection error logs
# because drivers may include sensitive connection metadata.
expect_failure() {
  local case_name="$1" expected_log="$2" candidate="$3" proc_id
  MANAGEMENT_DATABASE_URL="$candidate" "$TMP_DIR/management-api" > "$TMP_DIR/negative.log" 2>&1 &
  proc_id=$!
  local stopped=0
  for attempt in $(seq 1 100); do
    if ! kill -0 "$proc_id" 2>/dev/null; then
      stopped=1
      break
    fi
    sleep 0.1
  done
  if [[ "$stopped" != 1 ]]; then
    kill "$proc_id" >/dev/null 2>&1 || true
    wait "$proc_id" >/dev/null 2>&1 || true
    echo "FAIL $case_name: unsafe connection remained active" >&2
    exit 1
  fi
  if wait "$proc_id"; then
    echo "FAIL $case_name: unsafe startup returned success" >&2
    exit 1
  fi
  if ! grep -Fq "$expected_log" "$TMP_DIR/negative.log"; then
    echo "FAIL $case_name: rejection occurred at unexpected boundary" >&2
    exit 1
  fi
  if curl -fsS --max-time 1 http://127.0.0.1:18081/healthz >/dev/null 2>&1; then
    echo "FAIL $case_name: unsafe startup exposed HTTP listener" >&2
    exit 1
  fi
  echo "PASS $case_name"
}

expect_failure "untrusted signing CA" "management postgres unavailable" \
  "$DSN_PREFIX?sslmode=verify-full&sslrootcert=$TMP_DIR/untrusted-ca.pem"
expect_failure "DNS SAN mismatch" "management postgres unavailable" \
  "postgres://management_tls_login:$TEST_PASSWORD@127.0.0.1:55433/gpu_management_tls_ci?sslmode=verify-full&sslrootcert=$TMP_DIR/ca.pem"
expect_failure "TLS downgrade rejected before DB" "management postgres transport rejected" \
  "$DSN_PREFIX?sslmode=prefer&sslrootcert=$TMP_DIR/ca.pem"
expect_failure "missing CA rejected before DB" "management postgres transport rejected" \
  "$DSN_PREFIX?sslmode=verify-full"
expect_failure "DBA login rejected even with trusted TLS" "management postgres runtime authority rejected" \
  "postgres://gpu:ci-only-admin@localhost:55433/gpu_management_tls_ci?sslmode=verify-full&sslrootcert=$TMP_DIR/ca.pem"
echo "PASS all management-api real PostgreSQL TLS startup security controls"

# Prove the documented planned RESTART cutover, not zero-downtime hot reload:
# CA A -> CA B -> CA A. Postgres data/DB roles remain in the SAME persistent
# disposable container; switching the cert must not bypass the narrow LOGIN.
# Stop the DB before modifying any server private-key or certificate bytes.
cp "$TMP_DIR/server.crt" "$TMP_DIR/server-initial.crt"
sudo cp "$TMP_DIR/server.key" "$TMP_DIR/server-initial.key"
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout "$TMP_DIR/ca-rotated.key" -out "$TMP_DIR/ca-rotated.pem" \
  -subj "/CN=management-postgres-rotated-root" \
  -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1
openssl req -new -newkey rsa:2048 -sha256 -nodes \
  -keyout "$TMP_DIR/server-rotated.key" -out "$TMP_DIR/server-rotated.csr" \
  -subj "/CN=localhost" >/dev/null 2>&1
openssl x509 -req -days 1 -sha256 -in "$TMP_DIR/server-rotated.csr" \
  -CA "$TMP_DIR/ca-rotated.pem" -CAkey "$TMP_DIR/ca-rotated.key" \
  -CAcreateserial -out "$TMP_DIR/server-rotated.crt" \
  -extfile "$TMP_DIR/server.ext" >/dev/null 2>&1

# Writable original bind-mount INODES are retained across stop/start.
# tee (not mv) preserves PostgreSQL's owner-only key mode and bind mapping.
restart_with_server_cert() {
  local replacement_cert="$1" replacement_key="$2"
  docker stop "$PG_CONTAINER" >/dev/null
  cat "$replacement_cert" > "$TMP_DIR/server.crt"
  # Both the saved original key and live key may be root/PG-owned 0600.
  # Open source AND destination within the privileged process. A host
  # shell redirection would fail reading the protected rollback backup.
  sudo sh -c 'cat "$1" > "$2"' sh "$replacement_key" "$TMP_DIR/server.key"
  docker start "$PG_CONTAINER" >/dev/null
  local ready=0
  for attempt in $(seq 1 90); do
    if [[ "$(docker exec -e PGPASSWORD=ci-only-admin "$PG_CONTAINER" \
        psql -X -qAt -h 127.0.0.1 -U gpu -d gpu_management_tls_ci \
        -c 'SELECT 1' 2>/dev/null || true)" == 1 ]]; then
      ready=1
      break
    fi
    if ! docker inspect -f '{{.State.Running}}' "$PG_CONTAINER" 2>/dev/null | grep -qx true; then
      echo "FAIL restarted PostgreSQL TLS fixture stopped" >&2
      exit 1
    fi
    sleep 1
  done
  if [[ "$ready" != 1 ]]; then
    echo "FAIL target database unavailable after TLS certificate replacement" >&2
    exit 1
  fi
}
ROTATED_DSN="$DSN_PREFIX?sslmode=verify-full&sslrootcert=$TMP_DIR/ca-rotated.pem"
restart_with_server_cert "$TMP_DIR/server-rotated.crt" "$TMP_DIR/server-rotated.key"
expect_failure "stale CA A rejected after CA B rotation" "management postgres unavailable" "$VERIFIED_DSN"
expect_verified_login "new CA B accepted after server certificate rotation" "$ROTATED_DSN"

# Recovery is also strict: after rollback, the no-longer-trusted CA B must
# fail and the original verified CA A must work with the unchanged LOGIN.
restart_with_server_cert "$TMP_DIR/server-initial.crt" "$TMP_DIR/server-initial.key"
expect_failure "stale CA B rejected after CA A rollback" "management postgres unavailable" "$ROTATED_DSN"
expect_verified_login "original CA A works after certificate rollback" "$VERIFIED_DSN"
echo "PASS PostgreSQL TLS CA rotation and rollback acceptance (restart cutover; not zero downtime)"



# The supported Management image must also start against the real verified
# PostgreSQL TLS server as a NONROOT, READ-ONLY container. Linux host networking
# is used ONLY by this local disposable fixture; the process remains bound to
# 127.0.0.1 and is NOT approved for external exposure.
docker build -f Dockerfile --target management-api -t gpu-management-api:tls-ci .
IMAGE_CONTAINER="gpu-management-runtime-tls-ci-$$"
trap 'docker rm -f "$IMAGE_CONTAINER" >/dev/null 2>&1 || true; cleanup' EXIT INT TERM
IMAGE_ENV="$TMP_DIR/management-image.env"
cat > "$IMAGE_ENV" <<ENV
MANAGEMENT_DATABASE_URL=postgres://management_tls_login:$TEST_PASSWORD@localhost:55433/gpu_management_tls_ci?sslmode=verify-full&sslrootcert=/etc/management/pg-ca.pem
MANAGEMENT_LISTEN_ADDR=127.0.0.1:18081
OIDC_ISSUER=https://issuer.example.test/realms/gpu
OIDC_INTROSPECTION_URL=https://issuer.example.test/realms/gpu/protocol/openid-connect/token/introspect
OIDC_CLIENT_ID=disposable-management-ci
OIDC_CLIENT_SECRET=ci-only-secret
OIDC_AUDIENCE=management-api
ENV
chmod 600 "$IMAGE_ENV"
docker run -d --name "$IMAGE_CONTAINER" --network host \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --pids-limit 128 --memory 256m --env-file "$IMAGE_ENV" \
  --mount "type=bind,src=$TMP_DIR/ca.pem,dst=/etc/management/pg-ca.pem,readonly" \
  gpu-management-api:tls-ci >/dev/null
image_ready=0
for attempt in $(seq 1 100); do
  if curl -fsS --max-time 1 http://127.0.0.1:18081/healthz >/dev/null 2>&1; then
    image_ready=1
    break
  fi
  if ! docker inspect -f '{{.State.Running}}' "$IMAGE_CONTAINER" 2>/dev/null | grep -qx true; then
    echo "FAIL Management image could not start under strict TLS and runtime role" >&2
    exit 1
  fi
  sleep 0.1
done
if [[ "$image_ready" != 1 ]]; then
  echo "FAIL Management image failed local health check" >&2
  exit 1
fi
image_tls_count="$(docker exec "$PG_CONTAINER" psql -X -tA -U gpu -d gpu_management_tls_ci \
  -c "SELECT count(*) FROM pg_stat_activity a JOIN pg_stat_ssl s USING(pid) WHERE a.usename='management_tls_login' AND s.ssl IS TRUE")"
if [[ ! "$image_tls_count" =~ ^[1-9][0-9]*$ ]]; then
  echo "FAIL Management image has no verified live PostgreSQL TLS connection" >&2
  exit 1
fi
docker stop "$IMAGE_CONTAINER" >/dev/null
docker rm "$IMAGE_CONTAINER" >/dev/null
IMAGE_CONTAINER=""
echo "PASS nonroot read-only Management image + scoped runtime role + real verify-full PostgreSQL TLS"
