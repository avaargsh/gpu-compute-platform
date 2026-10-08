#!/usr/bin/env bash
# Run the frozen Stage B contract from a detached local worktree, without Actions.
# Usage: STAGE_B_EXPECTED_SHA=<40-char SHA> TEST_POSTGRES_DSN=postgres://... \
#          bash scripts/stage-b-local-gate.sh
set -uo pipefail

die() {
  printf 'LOCAL STAGE B GATE: BLOCKED: %s\n' "$1" >&2
  exit 2
}

# The Postgres contract tests execute schema setup and TRUNCATE.
# Only direct loopback connections to explicitly named *_test databases are
# allowed. Extra query params (host/port/dbname/service) may redirect pgx.
is_local_test_dsn() {
  local dsn="$1"
  [[ "$dsn" =~ ^postgres(ql)?://([^/@]+@)?(localhost|127\.0\.0\.1)(:[0-9]+)?/([a-zA-Z0-9_]*_test)(\?sslmode=(disable|require|verify-full))?$ ]]
}

# Run this independently, without Go, Git, PostgreSQL or an existing clone.
# The main gate also records it as a mandatory self-test before Go checks.
if [[ "${1:-}" == "--self-test" ]]; then
  for dsn in \
    "postgres://gpu:gpu@localhost:5432/gpu_platform_test?sslmode=disable" \
    "postgresql://gpu@127.0.0.1/another_test?sslmode=require" \
    "postgres://gpu@localhost/cas_test"; do
    is_local_test_dsn "$dsn" || die "safe test DSN was rejected: $dsn"
  done
  for dsn in \
    "postgres://gpu@10.0.0.5/gpu_platform_test" \
    "postgres://gpu@localhost/production" \
    "postgres://gpu@localhost/gpu_platform_test?host=10.0.0.5" \
    "postgres://gpu@localhost/gpu_platform_test?dbname=production" \
    "postgres://gpu@localhost/gpu_platform_test?port=5433" \
    "postgres://gpu@localhost/gpu_platform_test?sslmode=disable&host=10.0.0.5"; do
    if is_local_test_dsn "$dsn"; then
      die "unsafe test DSN was accepted: $dsn"
    fi
  done
  printf "LOCAL STAGE B GATE: DSN guard self-test PASS (3 allowed, 6 denied)\n"
  exit 0
fi
command -v git >/dev/null || die "git is required"
command -v go >/dev/null || die "go is required"
command -v sha256sum >/dev/null || die "sha256sum is required"
command -v mktemp >/dev/null || die "mktemp is required"

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || die "not inside a git checkout"
head_sha="$(git -C "$repo_root" rev-parse HEAD)" || die "cannot determine HEAD"
expected_sha="${STAGE_B_EXPECTED_SHA:-}"
[[ "$expected_sha" =~ ^[0-9a-f]{40}$ ]] || die "set STAGE_B_EXPECTED_SHA to the exact 40-character reviewed commit"
[[ "$head_sha" == "$expected_sha" ]] || die "HEAD $head_sha differs from frozen $expected_sha"
[[ -z "$(git -C "$repo_root" status --porcelain --untracked-files=normal)" ]] || die "working tree is not clean"
[[ -n "${TEST_POSTGRES_DSN:-}" ]] || die "TEST_POSTGRES_DSN is required; skipping PostgreSQL is not full acceptance"
if ! is_local_test_dsn "$TEST_POSTGRES_DSN"; then
  die "TEST_POSTGRES_DSN must use a localhost/127.0.0.1 *_test database with no query overrides (except sslmode); never point Stage B contracts at shared/production databases"
fi

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
evidence_root="${STAGE_B_EVIDENCE_DIR:-${TMPDIR:-/tmp}/stage-b-local-${head_sha:0:12}-$stamp}"
mkdir -p "$evidence_root" || die "cannot create evidence directory $evidence_root"
evidence_root="$(cd "$evidence_root" && pwd)"
worktree="$(mktemp -d "${TMPDIR:-/tmp}/stage-b-worktree-XXXXXXXX")" || die "cannot allocate temporary worktree"

cleanup() {
  git -C "$repo_root" worktree remove --force "$worktree" >/dev/null 2>&1 || true
  rmdir "$worktree" >/dev/null 2>&1 || true
}
trap cleanup EXIT

git -C "$repo_root" worktree add --detach "$worktree" "$head_sha" >"$evidence_root/worktree.log" 2>&1 ||
  die "cannot create detached worktree; see worktree.log"

printf 'head_sha\t%s\nstarted_utc\t%s\ncheckout\tdetached-worktree\npostgres_configured\ttrue\n' \
  "$head_sha" "$stamp" >"$evidence_root/run.tsv"
printf 'gate\tresult\texit_code\tlog_sha256\n' >"$evidence_root/gates.tsv"

failures=0
run_gate() {
  local gate="$1"
  shift
  local log="$evidence_root/$gate.log"
  local result rc digest
  if (cd "$worktree" && "$@") >"$log" 2>&1; then
    rc=0
    result=PASS
  else
    rc=$?
    result=FAIL
    failures=$((failures + 1))
  fi
  digest="$(sha256sum "$log")"
  digest="${digest%% *}"
  printf '%s\t%s\t%s\t%s\n' "$gate" "$result" "$rc" "$digest" >>"$evidence_root/gates.tsv"
  printf '%s: %s (exit %d)\n' "$gate" "$result" "$rc"
}

run_gate shell-syntax bash -n scripts/stage-b-local-gate.sh scripts/e2e/volcano-queue-apiserver-contract.sh
run_gate dsn-guard bash scripts/stage-b-local-gate.sh --self-test
run_gate format make fmt-check
run_gate vet go vet ./...
run_gate volcano-contract make stage-b-volcano-contract
run_gate volcano-race go test -race ./internal/provider/volcano
run_gate full-test go test ./...
run_gate acceptance make acceptance-contract
run_gate supply-chain make supply-chain-check
run_gate build make build

sha256sum "$evidence_root/run.tsv" "$evidence_root/gates.tsv" >"$evidence_root/manifest.sha256"
printf 'Evidence: %s\n' "$evidence_root"
if (( failures > 0 )); then
  printf 'LOCAL STAGE B GATE: FAIL (%d failed); no merge authorization\n' "$failures" >&2
  exit 1
fi
printf 'LOCAL STAGE B GATE: PASS for %s (CPU-only; not kind/Volcano proof)\n' "$head_sha"
