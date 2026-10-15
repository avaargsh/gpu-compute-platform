# One-hop real identity + database acceptance (disposable CI)

This acceptance uses an actual Chromium Authorization Code + **S256 PKCE**
redirect to **real HTTPS Keycloak**, returns an issuer-signed access token,
makes a real Management API request using RFC7662 introspection at the same
Keycloak, and persists desired state plus immutable operation receipt in
**PostgreSQL 17 over verify-full TLS** using the **actual constrained runtime
LOGIN**. There are **no OAuth or Management HTTP route mocks**.

Workflow: `.github/workflows/management-fullstack-real-oidc.yml`
Runner: `bash scripts/e2e/management-fullstack-real-oidc-pg.sh`
Browser: `frontend/tests/e2e/management.fullstack.e2e.spec.ts`

## Acceptance claims, no more

1. Real Keycloak HTTPS Discovery, username/password web login with S256
   challenge, auth-code/token exchange, exact issuer and audience verification.
   The introspection client secret stays in the Go process; the browser
   receives only its public client configuration and ephemeral bearer.
2. Production `cmd/management-api` starts on **127.0.0.1** with a genuine
   restricted PostgreSQL login, trusted database CA and verify-full;
   `pg_stat_ssl` independently reports an encrypted runtime connection.
3. An authenticated operator in a DBA-provisioned project submits a
   ComputePool Desired, obtains an operation ID and independently reads its
   append-only audit record from PostgreSQL. This proves a committed Desired
   operation only — not Kubernetes/GPU execution.
4. With the same still-valid browser bearer, a DBA downgrades the live grant
   to `viewer`: project role changes, mutation and owner receipt are
   denied. A full membership revocation removes project discovery and
   rejects resource read.

All Docker ports bind to loopback; test identity, account password, PostgreSQL
server TLS, key material and database live only for this CI job. The temporary
Keycloak realm has **public Authorization Code + PKCE client** only for
this test; the separate confidential introspection client is server-side.
Browser certificate errors are ignored **only by this disposable browser
test context**, while Go explicitly verifies the local Keycloak CA and DB
verifies its separate CA. Do not transfer this browser TLS setting to
production. Never copy the fixture test users or passwords into deployed
realms.

## Not production clearance

This is CI proof of the exact requested cross-dependency path, not an
approved TLS reverse proxy, Kubernetes tenant network isolation, user
provisioning workflow, IdP failover, refresh-token handling, rate limits,
backups, actual Kueue workload scheduling, or model readiness.

A production rollout still needs separate TLS ingress and browser-origin
review, secure OIDC client/secret provisioning, operational least-privilege
checks, project administration, cross-cluster authorization and failure
injection, and execution proof. No real GPU cluster is contacted.
