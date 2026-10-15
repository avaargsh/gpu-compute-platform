# Local static HTTPS Management gateway — isolated acceptance

**Status:** development-only / localhost-only / **NOT** a production public
ingress or approved multi-host Kubernetes deployment.

This slice uses **Adopt (Caddy)** rather than a new Go reverse proxy. It
builds the isolated Vue management workbench into static assets, serves
them over HTTPS on `127.0.0.1:3443`, and proxies **only**
`/api/v1/management/*` to the same-host loopback-only Go Management API
at `127.0.0.1:8081`. No Vite development server, anonymous Agent route,
remote Kubernetes API, or unprotected `/api/v1/internal` is reachable from
this gateway.

The strict Caddyfile lives at `deploy/local/management/Caddyfile`, with:
- Explicit `127.0.0.1` bind and a disposable CA-issued / locally generated
  IP-SAN TLS certificate; **no HTTP listener**.
- Exact static file route allowlist (`/management.html`, `/assets/*`);
  everything else gets 404.
- A single explicit management API reverse-proxy prefix, rejecting other
  paths even if a backing legacy Agent API exists on a different port.
- `no-store`, `nosniff`, frame denial, no-referrer and a restrictive CSP.
  The CSP's Keycloak URL is intentionally hardcoded to the CI issuer
  `https://127.0.0.1:8443`; do not reuse it unchanged in production.
- No forwarded identity headers can replace OAuth tokens: the Go process
  independently introspects the access token and checks PostgreSQL
  `management_project_grants` on every request.

The Management API accepts an **exact** opt-in
`MANAGEMENT_BROWSER_ORIGIN=https://127.0.0.1:3443` and continues to reject
remote/wildcard Origins, duplicate Origin headers, wrong ports, hostile
Sec-Fetch-Site and unsigned tokens. The existing explicit
`http://127.0.0.1:3002` local-only development flow remains available,
but cannot be mistaken for the HTTPS gateway Origin.

## Reproducible disposable acceptance

The GitHub Actions job
`.github/workflows/management-static-tls.yml` executes:

```bash
bash scripts/e2e/management-static-tls-acceptance.sh
```

The fixture creates **disposable Keycloak HTTPS, PostgreSQL 17
verify-full TLS and DB roles**, starts the real guarded Go Management API,
builds the real Vue bundle with a **public** OIDC Client ID and issuer
(never server secrets), then builds a locally tagged CI-only Caddy derivative pinned to an
upstream immutable SHA256, removing only the upstream executable's unnecessary
`cap_net_bind_service` file capability. It starts a nonroot, read-only,
`--cap-drop ALL --security-opt no-new-privileges` Caddy instance in a Linux
**host-network CI sandbox**. The separate build is needed because official
Caddy's file effective capability otherwise causes `execve EPERM` under full
capability-drop, even though this gateway listens on unprivileged port 3443.
The host-network exception is for **same-host numeric-loopback E2E only**,
not a deployment recommendation.

The tests check:
1. Static HTML over cert-verified TLS with CSP, no-store and nosniff.
2. Management path without Bearer fails with 401; Agent/internal and
   miscellaneous paths fail with 404.
3. Real Chromium goes to static HTTPS UI, redirects to the real Keycloak
   S256 PKCE login, exchanges an authorization code, and requests the same-
   origin Management API with a genuine issuer access token.
4. The actual non-privileged Go runtime connects to PostgreSQL using
   `sslmode=verify-full`; DB `pg_stat_ssl` confirms live TLS.
5. Resource Desired CAS write + append-only audit receipt commits.
   Changing DB role **while token is still valid** immediately denies
   mutations/receipt reads, and revoking membership denies project reads.

**Not proved:** production TLS/public DNS, proxy auth-hardening under
hostile network traffic, signed Caddy image or static artifact publication,
multiple network namespaces/hosts, secrets rotation, cert-manager,
Pod Security / Kubernetes NetworkPolicy, ingress-controller API semantics,
backup/recovery, Kueue admission, or successful GPU execution.

Do **not** publish a Kubernetes production manifest merely because this
one-host acceptance passes. In ordinary Docker bridge or Kubernetes Pods,
a backend bound to `127.0.0.1` is invisible to other Pods/namespaces:
do not 'fix' this by setting `0.0.0.0` or weakening the Go origin
guard. A production topology needs a separately reviewed shared
network namespace, Unix domain socket, or authenticated service-to-service
channel, **plus** externally scoped ingress and trust management.
