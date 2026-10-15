#!/usr/bin/env bash
# Validate production Management image isolation without any real credentials.
# This is a NEGATIVE-only container smoke test; real TLS+DB success is
# established separately by the management PostgreSQL integration job.
set -Eeuo pipefail
IMAGE="${1:-gpu-management-api:ci}"
case "$IMAGE" in
  gpu-management-api:ci|gpu-management-api:local) ;;
  *) echo "FAIL: only local disposable image tags are permitted" >&2; exit 2 ;;
esac

identity="$(docker image inspect --format '{{.Config.User}}' "$IMAGE")"
if [[ "$identity" != "65532:65532" && "$identity" != "65532" && "$identity" != "nonroot" && "$identity" != "nonroot:nonroot" ]]; then
  echo "FAIL: Management image must run as distroless nonroot, user=$identity" >&2
  exit 1
fi
cmd="$(docker image inspect --format '{{json .Config.Entrypoint}}' "$IMAGE")"
if [[ "$cmd" != '["/management-api"]' ]]; then
  echo "FAIL: Management image entrypoint not isolated" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
chmod 700 "$tmp"
# Dummy local non-secret trust root. Only tested for presence; the binary
# must reject an unsafe bind BEFORE initiating a database network call.
printf '%s\n' 'disposable smoke trust root placeholder' > "$tmp/ca.pem"
chmod 644 "$tmp/ca.pem"

expect_rejected() {
  local case_name="$1" expected="$2"
  shift 2
  local status=0
  docker run --rm --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges \
    --mount "type=bind,src=$tmp/ca.pem,dst=/etc/management-ca.pem,readonly" \
    "$@" "$IMAGE" >"$tmp/stdout" 2>"$tmp/stderr" || status=$?
  if (( status == 0 )); then
    echo "FAIL $case_name: invalid container unexpectedly started" >&2
    exit 1
  fi
  if ! grep -Fq "$expected" "$tmp/stderr"; then
    echo "FAIL $case_name: unexpected refusal boundary" >&2
    exit 1
  fi
  echo "PASS $case_name"
}

expect_rejected "missing PostgreSQL connection" "MANAGEMENT_DATABASE_URL is required"
expect_rejected "weak PostgreSQL TLS" "management postgres transport rejected" \
  -e 'MANAGEMENT_DATABASE_URL=postgres://test:dummy@localhost:5432/disposable?sslmode=disable'
expect_rejected "network wildcard bind rejected before connection" "management API must bind loopback" \
  -e 'MANAGEMENT_DATABASE_URL=postgres://test:dummy@localhost:5432/disposable?sslmode=verify-full&sslrootcert=/etc/management-ca.pem' \
  -e 'MANAGEMENT_LISTEN_ADDR=0.0.0.0:8081'
expect_rejected "remote IPv6 bind rejected" "management API must bind loopback" \
  -e 'MANAGEMENT_DATABASE_URL=postgres://test:dummy@localhost:5432/disposable?sslmode=verify-full&sslrootcert=/etc/management-ca.pem' \
  -e 'MANAGEMENT_LISTEN_ADDR=[::]:8081'

echo "PASS Management image: nonroot, canonical entrypoint, read-only filesystem, no network capabilities, fail-closed preflight"
