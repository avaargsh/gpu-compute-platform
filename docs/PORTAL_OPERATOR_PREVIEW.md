# Local operator preview — protected read-only Portal

This is a **single-operator, loopback-only pilot**. It closes the most immediate
gap between the Vue Portal and the Go Control Plane without creating a fake
login, duplicating Kueue, or inventing user/tenant identities.

This is **not** completed Issue #63. It is NOT OIDC, multi-user RBAC, full
project membership enforcement, production permission governance, a production
reverse proxy, or a production-ready portal. Agent endpoints on the same Go
server still use the pre-existing machine protocol, so **NEVER expose this Go
server on a network interface**.

## Start with an ephemeral operator credential

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
and nullable `error`. Fetching more uses an HMAC-authenticated cursor. It
is deterministic over a stable resource set, **not a snapshot-isolated
database transaction**; concurrent modifications can require a manual
refresh. The UI flags duplicate resources or changed counts.

Production comparison: see `docs/PORTAL_PRODUCT_BENCHMARK.md`.

## Security and data claims

- No token / bad token -> 401. Other cluster -> 404.
- Only configured project IDs are returned; rows without a valid project
  identity are hidden.
- Signed cursor tampering -> 400; limit max 120; page GET only.
- Only named read-only operator endpoint is exposed; mutations remain
  disabled. Legacy Agent API is unchanged and must remain private/loopback.
- `Synced` means matching generations, never model Ready, GPU assigned,
  Kueue Admitted, quota applied or llm-d Serving Ready.
- Project aggregates are derived from the pages loaded; declared accelerator
  quota is not actual GPU consumption or a cost/billing measurement.
- Credentials are kept only in memory until page refresh. Keep localhost
  access trusted; a shared/untrusted workstation is not an isolation boundary.
- No browser E2E or real hardware proof is claimed by the code/CI alone.

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
