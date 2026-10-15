# Local operator preview — protected read-only Portal

This is a **single-operator, loopback-only pilot**. It closes the most immediate
gap between the Vue Portal and the Go Control Plane without creating a fake
login, duplicating Kueue, or inventing user/tenant identities.

This is **not** completed Issue #63. It is NOT OIDC, multi-user RBAC, full
project membership enforcement, production permission governance, a production
reverse proxy, or a production-ready portal. Agent endpoints on the same Go
server still use the pre-existing machine protocol, so **NEVER expose this Go
server on a network interface**.

## Recommended: one-terminal local launch

Run from the repository root in a trusted interactive terminal:

```bash
PORTAL_OPERATOR_CLUSTER_ID=cluster-a \
PORTAL_OPERATOR_PROJECT_IDS=research,serving \
bash scripts/dev/portal-local.sh
```

The launcher checks prerequisites and local ports, uses the existing locked
Vue dependencies (runs `npm ci` only when the Vite executable is missing),
builds and starts the Go server and Vite on **127.0.0.1 only**, and prints a
fresh one-time operator token **only to an interactive terminal**. Enter the
same Cluster ID and displayed token at
`http://127.0.0.1:3001/portal.html`. Press Ctrl-C to stop both processes.

For noninteractive scripts, you **must** set `PORTAL_OPERATOR_TOKEN`
explicitly; the launcher will not print credentials into redirected logs.
It never adds demonstration resources. An empty in-memory Go store returns a
real cluster 404 until an actual Agent registers; set `DATABASE_URL` to
use an existing trusted development PostgreSQL store instead. Legacy Agent
endpoints still require localhost isolation and are **not authenticated**
by the Portal's operator token.

The accompanying `scripts/e2e/portal-local-launch-smoke.sh` tests process
startup, protected GET rejection, no token in logs, browser proxy isolation,
and automatic process cleanup against real local HTTP servers. The smoke test
does not claim GPU execution or project membership RBAC.

## Manual start with an ephemeral operator credential

Run from repo root (Linux/macOS Bash):

```bash
# Use a strong one-time random secret; do not commit or paste it into chat.
export PORTAL_OPERATOR_TOKEN="$(openssl rand -hex 32)"
export PORTAL_OPERATOR_CLUSTER_ID="cluster-a"
export PORTAL_OPERATOR_PROJECT_IDS="research,serving"
export CONTROL_PLANE_LISTEN_ADDR="127.0.0.1:8080"

# Use a disposable local database or the existing dev in-memory backend.
go run ./cmd/control-plane
```

Register a real Agent with the same cluster ID and at least one desired
ComputePool or Workload belonging to one of the allowed project IDs.

In a second terminal:

```bash
cd frontend
npm ci
npm run portal:test
npm run portal:dev
# http://127.0.0.1:3001/portal.html
```

Enter `cluster-a` and the same operator token into the portal's password
input (in-memory only; not saved). The `/api/v1/operator/...` endpoint
validates the token using a constant-time digest check, rejects any
unconfigured cluster, filters allowed project IDs on the **Go server**, and
returns one bounded page containing desired state and independent observation.
No frontend-localStorage auth or unauthenticated `/api/v1/agent/desired`
fetch is used by the new Portal.

The operator endpoint returns JSON with `agent, rows, total, truncated,
nextCursor, refreshedAt`. Each row has a `desired`, a nullable `state`
and nullable `error`. Fetching more uses an HMAC-authenticated cursor **bound to a SHA-256
fingerprint of the complete authorized Desired inventory**. A changed
Desired generation, spec, deletion state, or project scope returns **HTTP
409**, even if the record count stays unchanged. The Portal discards any
previously loaded partial table and requires the operator to re-read.
Invalid/forged cursors still return 400. Updates to excluded foreign-project
resources do not invalidate the authorized cursor.

This is **not** a database snapshot-isolated transaction: independent
observations can change between pages, and Desired may mutate while an
individual page is being produced. Never treat pagination coherence as
operation ownership, task readiness or a transactional inventory receipt.

Production comparison: see `docs/PORTAL_PRODUCT_BENCHMARK.md`.

## Security and data claims

- No token / bad token -> 401. Other cluster -> 404.
- Only configured project IDs are returned; rows without a valid project
  identity are hidden.
- Signed cursor tampering -> 400; limit max 120; page GET only.
- The browser Vite proxy exposes **only** `/api/v1/operator`; it never proxies
  legacy Agent/management mutation paths.
- In preview mode, cross-origin browser write requests and non-JSON machine
  POST/PUT/PATCH payloads are rejected (CSRF/form-post hardening). This is
  **not** authentication for Agent or other local machine clients.
- Operator endpoint is GET-only, while the underlying legacy Agent API
  remains on the loopback Go server and must never be published.
- `Synced` means matching generations, never model Ready, GPU assigned,
  Kueue Admitted, quota applied or llm-d Serving Ready.
- Project aggregates are derived from the pages loaded; declared accelerator
  quota is not actual GPU consumption or a cost/billing measurement.
- Credentials are kept only in memory until page refresh; changing the token
  or cluster clears the old resource list and discards in-flight results.
  A Lock button clears the operator credential. Keep localhost access trusted;
  a shared/untrusted workstation is not an isolation boundary.
- Chromium E2E proves the local disposable Go/Vue HTTP path, including
  pagination and stale-Desired 409; it does **not** prove real hardware,
  production authentication, or multi-user tenant RBAC.

## Required gates

```bash
go test ./cmd/control-plane ./internal/platform/httpapi
go test -race ./internal/platform/httpapi
cd frontend
npm ci
npm run portal:test
npm run portal:build
```

For **production** access: replace the preview credential with a mature OIDC
provider, verify tokens server-side, map project membership to authorized
claims/role assignment, enforce authz for all management reads and writes,
separate Agent and user routes, and test cross-tenant and stale role
denials in a real browser. These remain required in Issue #63.

Existing Stage B remains Kueue-only. llm-d's separate Draft #61 is not
automatically promoted by this Portal change.

CI note: PR #64 temporarily targets master for the existing Go/Portal workflow;
it must not merge ahead of independent acceptance of its Portal baseline #62.
