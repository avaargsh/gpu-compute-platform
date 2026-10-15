# Management Real-Dependency Security Acceptance

Status: **CI candidate**. Both workflows exercise real, disposable dependencies,
not production infrastructure. A green run proves only the explicitly tested
contract at the PR SHA. Real cluster/gateway traffic, actual browser login,
operational security review, trusted TLS ingress and tenant lifecycle remain
separate release gates.

## CI evidence

`.github/workflows/management-security-integration.yml` runs independently
of the general Go tests and is triggered by changes to the management API,
schema, scripts or the workflow:

1. **Keycloak HTTPS** (`management-keycloak-https.sh`) starts a disposable
   pinned Keycloak image, imports a *test-only realm*, obtains an access token,
   checks strict HTTPS issuer/audience/subject introspection, rejects a
   forged token and an audience mismatch, revokes the issued token, then
   verifies it can no longer be authorized. The test-only direct password
   grant is not the browser production login flow. Credentials in the fixture
   are deliberately disposable, not secrets for any real environment.
2. **PostgreSQL TLS** (`management-postgres-real-tls.sh`) provisions a
   disposable PostgreSQL 17 server, a signed server certificate, scoped
   application LOGIN and DB schema as a DBA. It boots the **actual**
   `cmd/management-api` executable using `sslmode=verify-full` and checks
   `pg_stat_ssl`. It rejects wrong CA, DNS hostname mismatch, TLS downgrade,
   missing trust root, and a DBA/superuser login. It checks the planned
   CA A → CA B → CA A restart cutover and verifies stale trust roots are
   rejected on both transitions. This is **not** live zero-downtime rotation.

Both scripts clean up temporary DB/IdP containers and generated test keys.
The management HTTP listener remains bound only to loopback and never exposes
unauthenticated Agent endpoints. No production credentials or data are used.

## Not yet accepted

These are independent tests: Keycloak and PostgreSQL are **not** wired together
in one user workflow yet. A separate
`.github/workflows/management-browser-postgres.yml` runs real Chromium
against the real Vite localhost proxy, actual Go Management handler and an
ephemeral PostgreSQL 17 database, asserting Pool/Workload Desired commits,
CAS errors, viewer denial, operation receipts and safe reload/recovery.
**Its OAuth code issuance and Go token introspection are TEST DOUBLES**,
not Keycloak identity proof. The fixture refuses non-disposable,
non-loopback DB names and cannot start outside an explicit test-only gate.
Together these jobs cover the independent IdP, DB and browser/backend
contracts, but they still do NOT prove a single complete real-IdP+PG
interactive PKCE session. They do not prove interactive browser PKCE,
multi-user project revocation races over actual Keycloak/Postgres, end-to-end
deletion success, Kubernetes scheduling, model readiness or GPU execution.

Keep developer double-token mode restricted to loopback until those
boundaries are proven. Fail closed; do not relax OIDC claims or DB privileges
merely to make integration tests pass.
