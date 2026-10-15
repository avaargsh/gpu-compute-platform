# S0 Project-scoped read API — OAuth introspection PoC

**Status: implementation candidate only. No production approval.**
**Base:** stacked on #80 (itself stacked on #78). **Tracking:** #63, #79.
**Not an OIDC ID token verifier:** this slice uses server-side **OAuth 2.0 RFC 7662 access-token introspection**. Only deploy with an identity provider that supports secure introspection, such as appropriately configured Keycloak. Later OIDC ID token / JWKS support requires a mature, independently reviewed library. Never trust decoded JWT claims from a browser.

## Product boundary

New `poc` HTTPS listener exposes **only**:

```text
GET /healthz
GET /api/v1/projects/{projectID}/compute-pools
GET /api/v1/projects/{projectID}/compute-pools/{resourceID}
GET /api/v1/projects/{projectID}/workloads
GET /api/v1/projects/{projectID}/workloads/{resourceID}
```

Everything else (Agent APIs, globally scoped GET, writes, migration, project binding updates, internal desired endpoints) is unreachable on the human listener. Agent mTLS and human OAuth MUST NOT share the same PoC listener. The unsecured development-only Router is preserved for deterministic tests, but still loopback-only and never a safe public endpoint.

Every GET:
1. Accepts exactly one `Authorization: Bearer <access-token>` header. Ignores `X-Forwarded-User` and role/project claims.
2. Sends the opaque access token via authenticated HTTPS POST to the configured OAuth introspection endpoint (no redirects, no token logging).
3. Requires `active=true`, a non-empty subject, an expected audience, a future numeric expiration, a valid not-before if supplied, and an issuer compatible with the configured authority. The trusted issuer is pinned to configuration, not the caller's assertion.
4. Queries `platform_project_memberships` using **(issuer, subject, project_id)** and `active=true` on every request. Roles owner/developer/viewer may read. A membership error fails closed with 503.
5. Resolves the requested Project binding and only reads Desired/Observed records from that bound cluster. A record must explicitly belong to the requested Project; its ComputePool binding must also match the cluster. Foreign resources/projects and missing IDs return privacy-preserving 404.
6. Projects only public ComputePool/Workload view fields, with Desired/Observed generation and sync state. **Synced is NOT Ready.** Never returns raw Desired `Spec` or internal lease owner.

List endpoints sort by stable resource ID and use an opaque base64url keyset cursor bound to Project and kind; `pageSize` defaults to 20 and is capped at 100. This cursor is intentionally NOT an auth token and provides no snapshot isolation across concurrent creates/deletes. For large clusters, replace the current filtered-in-process `Desired(cluster)` query with indexed server-side SQL before production.

## Bootstrap prerequisites

- Nonproduction isolated CNPG test database with required tables from `internal/store/postgres/schema.sql` and `platform_project_memberships`.
- An external OAuth provider with RFC 7662 introspection. Register a confidential client allowed to introspect access tokens for the `gpu-api` audience. Store that client's secret as a mounted file (never in Git).
- Trusted HTTPS listener server certificate matching the local client DNS/IP SAN.
- Platform Go service DB connection must follow #78's PoC `sslmode=verify-full`, client cert, key and root CA requirements.
- Trusted administrator must create memberships out-of-band for this PoC. **No API route may grant itself project membership.**

Example *shape only*, not deployable without certificates and a configured identity provider:

```bash
export CONTROL_PLANE_PROFILE=poc
export CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:8080
export DATABASE_URL='postgres://platform_runtime@platform-db-rw.security.svc:5432/platform?sslmode=verify-full&sslrootcert=/etc/pg/ca.crt&sslcert=/etc/pg/tls.crt&sslkey=/etc/pg/tls.key'
export CONTROL_PLANE_OAUTH_INTROSPECTION_URL='https://idp.example/realms/gpu/protocol/openid-connect/token/introspect'
export CONTROL_PLANE_OAUTH_ISSUER='https://idp.example/realms/gpu'
export CONTROL_PLANE_OAUTH_AUDIENCE='gpu-api'
export CONTROL_PLANE_OAUTH_CLIENT_ID='gpu-platform-poc'
export CONTROL_PLANE_OAUTH_CLIENT_SECRET_FILE='/etc/idp/client-secret'
export CONTROL_PLANE_HUMAN_TLS_CERT='/etc/https/server.crt'
export CONTROL_PLANE_HUMAN_TLS_KEY='/etc/https/server.key'
./control-plane
```

The example issuer/audience are placeholders. Confirm that the chosen IdP actually returns the audience and expiration fields from introspection; do not weaken verification to make provider output pass. The PoC permits either Agent mTLS or human introspection listener per process, **not both**.

## Database enrollment

Use a privileged, reviewed migration/administration process (not the runtime DB role) to create membership records:

```sql
-- Example: after independent verification of a user's identity and Project assignment.
INSERT INTO platform_project_memberships(issuer, subject, project_id, role)
VALUES ('https://idp.example/realms/gpu', 'verified-oidc-sub', 'project-1', 'viewer')
ON CONFLICT (issuer, subject, project_id) DO UPDATE
SET role = EXCLUDED.role, active = TRUE, updated_at = now();
```

For revocation: `UPDATE ... SET active = FALSE, updated_at = now()` via an authorized administrative workflow. The application runtime role receives SELECT only on this table; never INSERT/UPDATE/DELETE. Migration/DDL role remains separate.

## Negative-first contract

- No access token, forged HTTP identity header, duplicate Authorization, invalid/expired token or wrong audience -> 401.
- Foreign issuer and active user with no Project membership -> 404; membership store outage -> 503.
- Project A token may not list or read Project B's ComputePool/Workload, even if both are scheduled on one cluster.
- Forged Project ID in path, wrong Project cursor, wrong kind cursor or invalid page size -> denied.
- Human bearer token cannot access Agent, migration, binding write or internal desired interfaces.
- All POST/PUT/PATCH/DELETE routes are unavailable; replay and lifecycle contracts remain under the previous Go/Agent route.
- Run `make fmt-check`, `make vet`, `make test`, `make acceptance-contract`, `make supply-chain-check`, `make e2e-golden` and independently repeat on the exact candidate SHA.

## Not production-ready until

- Integrate a mature audited OIDC/JWKS verifier or explicitly accept OAuth introspection as the chosen production auth contract, with IdP outage and rate-limit SLO.
- Fully implement audit event persistence (allow/deny), consistent correlation IDs and reliable trace custody.
- Reconcile tenant/project membership from the canonical identity authority rather than hand-editing the table.
- Add load-tested, indexed pagination/snapshot semantics; privacy review of evidence/status projections.
- Complete write-side Project RBAC and Agent certificate rotation/revocation, CNPG PITR and independently reviewed TLS ingress.
- Lift the #78 production startup guard **only after** these independent requirements pass.

**Reference:** RFC 7662 https://www.rfc-editor.org/rfc/rfc7662
