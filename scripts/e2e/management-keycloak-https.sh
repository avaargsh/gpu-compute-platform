#!/usr/bin/env bash
# TEST-ONLY HTTPS provider interoperability. No mock HTTP introspection.
# Keycloak import carries DISPOSABLE CI credentials and a test-only direct
# grant; production browser still uses Authorization Code + S256 PKCE.
set -Eeuo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
TMP_DIR="$(mktemp -d)"
CONTAINER="gpu-management-keycloak-ci-$$"
cleanup() {
  trap - EXIT INT TERM
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM
chmod 755 "$TMP_DIR"
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -keyout "$TMP_DIR/key.pem" -out "$TMP_DIR/cert.pem" \
  -subj "/CN=127.0.0.1" -addext "subjectAltName=IP:127.0.0.1" >/dev/null 2>&1
chmod 644 "$TMP_DIR/key.pem" "$TMP_DIR/cert.pem"
ISSUER="https://127.0.0.1:8443/realms/gpu-management-e2e"

docker run -d --rm --name "$CONTAINER" --memory 1536m \
  -p 127.0.0.1:8443:8443 \
  -e KC_BOOTSTRAP_ADMIN_USERNAME=ci-only-admin \
  -e KC_BOOTSTRAP_ADMIN_PASSWORD=ci-ephemeral-admin-do-not-use \
  -v "$ROOT/scripts/e2e/keycloak-management-realm.json:/opt/keycloak/data/import/gpu-management-e2e-realm.json:ro" \
  -v "$TMP_DIR/cert.pem:/opt/keycloak/conf/e2e-cert.pem:ro" \
  -v "$TMP_DIR/key.pem:/opt/keycloak/conf/e2e-key.pem:ro" \
  quay.io/keycloak/keycloak:26.8.0 start-dev --import-realm \
  --hostname=https://127.0.0.1:8443 \
  --https-port=8443 --http-enabled=false \
  --https-certificate-file=/opt/keycloak/conf/e2e-cert.pem \
  --https-certificate-key-file=/opt/keycloak/conf/e2e-key.pem >/dev/null

READY=0
for attempt in $(seq 1 120); do
  if curl --silent --show-error --fail --max-time 2 --cacert "$TMP_DIR/cert.pem" \
       "$ISSUER/.well-known/openid-configuration" > "$TMP_DIR/discovery.json" 2>/dev/null; then
    READY=1
    break
  fi
  if ! docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -qx true; then
    docker logs --tail 100 "$CONTAINER" >&2 || true
    echo "Keycloak integration container stopped unexpectedly" >&2
    exit 1
  fi
  sleep 2
done
if [[ "$READY" != "1" ]]; then
  docker logs --tail 100 "$CONTAINER" >&2 || true
  echo "Keycloak HTTPS Discovery did not become ready" >&2
  exit 1
fi

export KEYCLOAK_E2E_ISSUER="$ISSUER"
export KEYCLOAK_E2E_CA="$TMP_DIR/cert.pem"
go test -count=1 -v -run '^TestKeycloakRealHTTPSIntrospectionAndRevocation$' ./internal/platform/management
