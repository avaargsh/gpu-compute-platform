# Go Control Plane Portal (local, read-only)

**Status: developer-only integration slice. NOT authenticated, NOT production-ready.**

This is a second entry point inside the repository's **existing** Vue 3 /
Element Plus frontend. It does not create another frontend repository, copy
llm-d's router, or revive the legacy Python control-plane APIs.

The existing `frontend/src/main.ts` and `frontend/index.html` still belong
to the legacy Python-facing UI. The new Go-backed portal starts from
`frontend/portal.html`, uses the explicit Go HTTP contract and deliberately
does **not** treat the old Python login token as authorization to the Go API.

## Start locally

Run the existing Go server (port 8080) with a disposable local PostgreSQL DB,
or its documented in-memory development store, and register a Cluster Agent.

```bash
# Terminal 1, repo root: local Go API only
go run ./cmd/control-plane

# Terminal 2, repo root
cd frontend
npm ci
npm run portal:dev
# http://127.0.0.1:3001/portal.html
```

Enter an **existing** Cluster ID. There are no synthetic resource fixtures in
the Portal. Vite proxies only `/api/v1` to `http://127.0.0.1:8080`, and
the developer server binds exclusively to loopback.

```bash
cd frontend
npm run portal:build
```

The CI `portal` job typechecks and builds this isolated entry, separately
from the legacy Python UI.

## Real API / behavior

| Portal panel | Existing Go API | Trust boundary |
| --- | --- | --- |
| Cluster / heartbeat / capabilities | `GET /api/v1/clusters/{clusterID}/status` | Heartbeat recency is an estimate, not authoritative liveness |
| Existing pools and workloads | `GET /api/v1/agent/desired?clusterId=...` | Desired resources only; not proof of scheduling |
| Per-resource state and evidence | `GET /api/v1/internal/clusters/{clusterID}/state/{kind}/{id}` | Synced generation != Ready / Admitted / GPU allocated |
| Ecosystem page | Agent `capabilities.providers` and `schedulers` | Discovered scheduler != registered executable provider |
| llm-d | Explicitly unintegrated | Separate Draft PR #61; no claimed GAIE/EPP/Router health |

The client permits only simple resource-path segments, limits its observation to
120 supported resources with at most 6 simultaneous detail GETs, and labels
truncation instead of presenting a partial count as a full-cluster inventory.
Read failures are shown; no mock metrics, auth token, or fake success values
are inserted.

## Security and deliberate non-goals

The current Go server has no verified user auth or RBAC protecting these
read APIs. Consequently, this entry must be used **only against local trusted
development services**. The Portal does not implement login, provide a write
button, store credentials, or bypass any authentication boundary. **Do not
expose port 3001 or the unauthenticated Go API on a public network.**

This is a **read-only inventory and evidence slice**, not a finished GPU
management product. It does not implement resource create/delete, billing,
actual GPU telemetry, llm-d Serving deployment, automatic GPU provisioning,
tenant RBAC, server-side list pagination, or production HA.

## Promotion gates

Before any production Portal release:

1. Enforce Go API authentication, tenant/project isolation, and server-side
   authorization for both read and mutation endpoints. The legacy Python
   frontend's localStorage token is not an acceptable shortcut.
2. Introduce a dedicated paginated management-plane list API; stop relying on
   the Agent desired-state endpoint for Portal inventory.
3. Prove explicit user-authorized create/update/delete with generation CAS,
   stable binding, lifecycle/finalizer and audit evidence. No blind CRUD.
4. Independently test browser E2E, unauthorized/foreign-tenant read denial,
   stale generation 409, partial API failure, and disconnected Agent behavior.
5. Maintain the frozen Kueue-only v0.1 Golden Path. Add llm-d/KServe only as
   separately accepted Serving capabilities with real-cluster proof.

Do not silently convert this local preview into an Internet-accessible
management console.
