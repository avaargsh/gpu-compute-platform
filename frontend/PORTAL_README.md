# Go Control Plane Portal (local, read-only)

**Status: developer-only integration slice. NOT authenticated, NOT production-ready.**

This is a second entry point inside the repository's **existing** Vue 3 /
Element Plus frontend. It does not create another frontend repository, copy
llm-d's router, or revive the legacy Python control-plane APIs.

The existing `frontend/src/main.ts` and `frontend/index.html` still belong
to the legacy Python-facing UI. The new Go-backed portal starts from
`frontend/portal.html`, uses the **operator-scoped Go snapshot API**, and
deliberately does **not** treat the old Python login token as authorization.
For an up-to-date runnable local setup and its strict limits, read
`docs/PORTAL_OPERATOR_PREVIEW.md`.

## Start locally

Run the existing Go server (port 8080) with a disposable local PostgreSQL DB,
or its documented in-memory development store, and register a Cluster Agent.

```bash
# Terminal 1: strong ephemeral preview token, exact cluster/project scope
export PORTAL_OPERATOR_TOKEN="$(openssl rand -hex 32)"
export PORTAL_OPERATOR_CLUSTER_ID=cluster-a
export PORTAL_OPERATOR_PROJECT_IDS=research,serving
CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/control-plane

# Terminal 2, repo root
cd frontend
npm ci
npm run portal:dev
# http://127.0.0.1:3001/portal.html
```

Enter the **configured** Cluster ID and its in-memory operator token.
The Go snapshot endpoint applies project scoping and signed pagination
server-side; there are no synthetic resource fixtures. Vite proxies only
`/api/v1/operator` to `http://127.0.0.1:8080` and binds to loopback.
The Portal also clears a previously loaded inventory when the operator token
or cluster changes, and offers a Lock control.

```bash
cd frontend
npm run portal:test
npm run portal:build
```

The CI `portal` job typechecks and builds this isolated entry, separately
from the legacy Python UI.

## Real API / behavior

| Portal panel | Existing Go API | Trust boundary |
| --- | --- | --- |
| Cluster / heartbeat / capabilities | `GET /api/v1/operator/clusters/{clusterID}/snapshot` (Bearer preview token) | Heartbeat recency is an estimate, not authoritative liveness |
| Existing pools and workloads | Same scoped snapshot endpoint | Server-enforced project allowlist; not full user RBAC |
| Per-resource state and evidence | Same scoped snapshot endpoint | Synced generation != Ready / Admitted / GPU allocated |
| Project views | Existing Desired `spec.projectID` | Client-only grouping, **never** RBAC or authoritative tenant isolation |
| Operations Inbox | Independent State + Conditions + GET errors | Evidence-based triage; `Synced` != Ready, read failure != resource failure |
| Ecosystem page | Agent `capabilities.providers` and `schedulers` | Discovered scheduler != registered executable provider |
| llm-d | Explicitly unintegrated | Separate Draft PR #61; no claimed GAIE/EPP/Router health |

The Go snapshot API limits each page to 120 records and issues a signed
nextCursor. The Portal loads more only when explicitly requested. Partial
counts are labeled as a loaded sample. The operator token exists only in
component memory, never in localStorage, and is required for every request.
There are no synthetic metrics or false success values.

## Security and deliberate non-goals

The Go preview snapshot now has a server-checked operator token, cluster
identity and project allowlist, but the **existing Agent API routes on the
same Go server have NOT gained user authentication**. This is therefore
**loopback-only developer access**, not a deployable OIDC/RBAC solution.
The Portal provides no write button. **Never expose either port 3001 or
port 8080 to a public or untrusted network.**

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

Product parity and UI decision record: `docs/PORTAL_PRODUCT_BENCHMARK.md`.
All quota aggregates in the local UI are **declared** ComputePool intent, not
GPU utilization, billed usage, available capacity, or Kueue acceptance.
