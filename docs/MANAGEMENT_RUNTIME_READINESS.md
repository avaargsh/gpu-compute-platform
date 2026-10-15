# Management process liveness versus runtime-authority readiness

This slice exposes two **direct loopback-only** health endpoints on the
independent Go Management API:

| Endpoint | Result | What it proves |
|---|---|---|
| `GET /healthz` | 204 while handler process is alive | **Liveness only**. Never use as a readiness or rollout success signal |
| `GET /readyz` | 204 only when the current Postgres runtime login passes **fresh, comprehensive effective-privilege admission**; otherwise 503 | DB connectivity + required scoped grants/functions; forbids DBA/owner/unsafe role identity and excess privileges |

No authentication token, SQL error, database URL, project name or
role details are exposed by either path. Responses are no-store; readiness
also uses `X-Content-Type-Options: nosniff`. The probe has a two-second
timeout, uses the same `VerifyRuntimeDBPrivileges` policy as guarded
mutations, and **does not mutate desired state**.

## Why readiness is separate

A live process may lose its database connection, acquire an unsafe grant,
lose the append-only audit INSERT right, or be switched to a privileged
login. Continuing to report readiness after that would mislead a future
operator/control-plane rollout.

The real disposable PostgreSQL integration checks:

```text
real scoped PostgreSQL LOGIN + verify-full TLS
  /healthz = 204 | /readyz = 204

DBA revokes management_operation_audit INSERT:
  /healthz = 204 | /readyz = 503

DBA restores only the needed INSERT:
  /healthz = 204 | /readyz = 204
```

The test changes only disposable CI database grants, not a real customer
environment. The existing startup and per-mutation checks remain
authoritative: **readiness is not itself an authorization proof for
individual HTTP mutations**. It also does not prove the IdP is online,
the OIDC token is active, Caddy/TLS edge is reachable, Kueue accepts
workloads, or GPU execution succeeded.

The Caddy static HTTPS gateway deliberately **does not proxy** either
`/healthz` or `/readyz`: both paths must be 404 on its frontend. A
future separately reviewed local process supervisor may probe the Go
listener within its trusted network namespace, but these endpoints
should not be exposed as unauthenticated public Internet checks.

**Rollout rule:** never use `/healthz` in place of `/readyz`, and never
turn a green readiness response or a Desired commit into an observed
Kubernetes execution-success assertion.
