# Local Management Workbench — OAuth Code + PKCE Pilot

**Status:** developer-only slice; NOT production-ready. This is a separate
standalone browser app from the original read-only `portal.html`, and it does
not weaken the legacy Cluster Agent / Kueue security boundary.

## What works

- Browser redirects to an explicitly configured HTTPS OIDC issuer, using
  Authorization Code + **S256 PKCE**, random state and nonce.
- Only a single short-lived access token is kept in Vue memory. A one-use
  verifier/state/nonce tuple is kept in tab-local sessionStorage **only
  across the redirect**, consumed on callback. Authorization codes are
  immediately removed from the browser URL before token exchange.
- The browser itself does not trust decoded JWT claims or client-supplied
  roles. The separate Go Management API introspects the access token
  on **every** request, checks expiry, issuer and configured audience, and
  enforces PostgreSQL project roles.
- Browser can discover the currently authorized projects and their Cluster ID
  using `GET /api/v1/management/projects`, which joins current PostgreSQL
  `management_project_grants` with `project_bindings` for the introspected
  issuer and subject. No browser-provided role is trusted; a viewer sees only
  their own viewer scopes and mutation permissions are checked again per
  request. The response is `{"projects":[{"projectId":"research",
  "clusterId":"cluster-a","role":"operator"}],"truncated":false}`. The
  first 100 authorized projects are returned in stable order and
  `truncated:true` explicitly warns if more exist.
- Browser can GET one scoped Desired resource, PUT a typed resource request
  with a fresh random `Idempotency-Key`, DELETE using the exact
  Generation as `If-Match`, and query a committed-operation audit receipt.
- No automatic retries for mutations. Before a PUT/DELETE leaves the
  browser, its 128-bit operation ID and resource scope are written to
  **tab-local sessionStorage**, with 24-hour expiry and **only one record**.
  A browser reload discards the access token; even the saved resource
  metadata remains **hidden until fresh OIDC sign-in**. The operator can
  then choose **恢复操作范围与 ID** and query the original audit receipt.
  No Token, request JSON/spec, password, PKCE verifier or secret is persisted
  with the operation pointer. If sessionStorage cannot be written, the
  browser **refuses to issue the mutation**.
- A receipt still requires current server-side OIDC identity and project
  RBAC; the sessionStorage pointer is **NOT** proof of a committed Desired
  write and grants no authority. Missing/expired pointers, closing the tab,
  explicit sign-out, clearing browser data and issuing a new operation
  may lose the previous ID. Keep an independent trusted operator journal
  for important tasks. A 404 receipt does NOT prove there is no in-flight
  commit. Never reuse an ID with a different request body and do not mint
  a new ID to retry an ambiguous outcome.
- Same-origin Vite dev proxy allows ONLY the separate authenticated
  `/api/v1/management` prefix to `127.0.0.1:8081`, **not** legacy
  `/api/v1/agent` or `/api/v1/internal`.

## Prerequisites

1. A working PostgreSQL test database with the regular Go Store schema and
   `internal/platform/management/schema.sql` applied by a trusted DBA.
2. An independently configured OIDC provider with an **HTTPS** issuer URL
   that supports public Authorization Code + PKCE S256 clients. For
   Keycloak this is a public client with S256 required and the exact redirect
   URI `http://127.0.0.1:3002/management.html` and Web Origin
   `http://127.0.0.1:3002`. Never enable wildcard redirect URIs.
3. The provider must permit browser CORS to its OIDC Discovery and Token
   endpoint. It must also issue an **access** token whose introspection
   response includes `active, iss, sub, aud, exp, token_type=Bearer`,
   where `aud` contains the configured `OIDC_AUDIENCE`. Not every IdP
   returns these optional RFC 7662 fields by default: configure explicit
   provider audience / protocol mappers rather than relaxing API validation.
4. A separate confidential client, not the browser public client, with
   introspection permission and secret available **only to the Go server**.
5. Provision an explicit `management_project_grants` row for the tested
   issuer, user subject, project, and `viewer/operator/admin` role. Also
   provision valid `project_bindings` and `cluster_bindings` consistent
   with the existing Kueue-only provider contract.

The browser app never receives the confidential introspection client secret
or any database credentials.

## Start on loopback only

From the repository root, in separate trusted terminals:

```bash
export MANAGEMENT_DATABASE_URL='postgres://...'
export OIDC_ISSUER='https://idp.example.test/realms/gpu'
export OIDC_INTROSPECTION_URL='https://idp.example.test/realms/gpu/protocol/openid-connect/token/introspect'
export OIDC_AUDIENCE='gpu-management'
export OIDC_CLIENT_ID='management-introspection-confidential'
export OIDC_CLIENT_SECRET='...'            # server only; never in VITE_ vars
export MANAGEMENT_LISTEN_ADDR='127.0.0.1:8081'
export MANAGEMENT_BROWSER_ORIGIN='http://127.0.0.1:3002' # explicitly allow local browser writes
go run ./cmd/management-api
```

```bash
cd frontend
npm ci
export VITE_MANAGEMENT_OIDC_ISSUER='https://idp.example.test/realms/gpu'
export VITE_MANAGEMENT_OIDC_CLIENT_ID='gpu-management-public'
npm run management:dev
# open http://127.0.0.1:3002/management.html
```

**These are intentionally separate OIDC clients:** the first is a
confidential server-to-provider introspection client; the second is a public
browser PKCE client. The issuer and audience must agree across both. Setting
`VITE_MANAGEMENT_OIDC_CLIENT_SECRET` or sending DB credentials to the
browser is forbidden.

### Browser Origin and local proxy requirement

Real browsers send an `Origin` header on PUT/DELETE. The Management API
**rejects all Origin-bearing mutations by default**. For the local workbench,
the operator must opt in with exactly
`MANAGEMENT_BROWSER_ORIGIN=http://127.0.0.1:3002`, passed **only to the Go
management-api process**. It is not an OIDC grant or project authorization
mechanism; verified Bearer token introspection, current project RBAC, CAS,
idempotency and atomic audit are still mandatory for every mutation.

The Go handler rejects a different Origin, `null`, duplicate Origin headers,
`Sec-Fetch-Site: cross-site`, any wildcard, any external host, and any
malformed origin configuration. The CLI omits `Origin` and remains supported
without this opt-in. The Vite development proxy preserves the browser Origin
while routing ONLY `/api/v1/management` to loopback Go.

This is a loopback pilot exception, **not a permitted production CORS,
reverse proxy or public TLS configuration**. Do not weaken the restriction
or add `Access-Control-Allow-Origin: *`.

## Operator acceptance

1. Sign in using the provider redirect. The local browser page never
   accepts a password and does not ask for an operator preview secret.
2. Click **查询当前账号的项目** and select a currently authorized
   Project/Cluster entry. This selection invalidates any previously loaded
   resource Generation and operation ID. Manual entry remains supported if
   the capped project list is incomplete; it never grants server access.
3. Create a ComputePool by specifying JSON including
   `expectedGeneration: 0` and the provider-compatible Kueue binding spec.
   The API creates only Desired resource state, not an executed job.
4. Read the same resource. Note its returned Generation.
5. Query the displayed operation ID. Verify
   `status: "desired-committed"` and matching resource/scope/generation.
6. Create a Workload against the pool, then attempt to DELETE the pool:
   the API must reject deletion while dependent Workloads exist.
7. Read a Workload, confirm DELETE and inspect its receipt.
   HTTP 202 records a deletion request/finalizer, not proof of physical
   teardown. Inspect the *independent* observation and Golden Path evidence
   before asserting actual cleanup.
8. Revoke the user's project grant, repeat a write and receipt query.
   Writes must fail 403, own old receipts must be inaccessible.
9. Test a stale Generation / conflicting operation ID: must return 409
   and produce no additional successful audit row.

## Real Keycloak HTTPS interoperability CI (server token path only)

The Go CI adds a **separate disposable Keycloak 26.8.0** container, with a
self-signed HTTPS certificate trusted ONLY by the integration test. Start and
tear down is automated through:

```bash
bash scripts/e2e/management-keycloak-https.sh
```

The process is bound to `127.0.0.1:8443`; its realm import lives at
`scripts/e2e/keycloak-management-realm.json`. Import credentials and
`grant_type=password` are **explicitly disposable test fixtures** for obtaining
a REAL issued Keycloak access token. Password grants are NOT enabled for the
production browser, which continues to use Authorization Code + S256 PKCE.
Do not deploy or reuse the disposable realm/password/secret values.

`TestKeycloakRealHTTPSIntrospectionAndRevocation` exercises the existing
`Introspector.Verify` against the real confidential-client HTTPS endpoint,
not an httptest HTTP double. The issuing test client has an explicit audience
mapper for `gpu-management-api` and `gpu-e2e-introspection`; both are needed
to respect modern Keycloak introspection audience restrictions. Check:
successful exact issuer + subject + API audience, reject another API audience,
reject a non-issued token, and reject a token following test-only revocation.
Token bodies and access-token secrets must never be printed in CI logs.

The test client injects a temporary CA into its dedicated Go HTTP transport.
**Production** `management-api` still requires a trusted HTTPS identity
provider and does NOT enable `AllowHTTPForTests` or skip TLS verification.

A green result here means real Keycloak **server-side token introspection
interoperability only**. It does NOT prove full real browser-based SSO, MFA,
ID-token nonce validation, public TLS ingress or production network policy.

## Hard release blockers

- This slice has **not** yet been exercised with a real Keycloak / Dex /
  enterprise IdP, consent/MFA policy or token audience mapper in CI.
- The browser does **not** validate the ID token or its nonce independently.
  It is intentionally an OAuth access-token PKCE path backed by server-side
  token introspection, **not a completed production OIDC session system**.
- No refresh token or persistent browser session. Reloading requires a new
  sign-in. Clearing browser memory does NOT revoke the provider SSO session.
- The single most-recent operation pointer survives **same-tab reload**
  within 24 hours but is **not a durable journal**: tab close, browser
  eviction, logout or the next operation may remove it. Request specs
  and tokens are deliberately never stored. For 404/ambiguous outcomes,
  the browser cannot safely replay a mutation automatically; operators
  need the separately saved original request body and CAS inputs.
  No exactly-once client guarantee across browser or device crashes.
- No server-side CSRF cookie because bearer credentials are sent explicitly
  with `credentials: omit`; a full public deployment needs reviewed TLS,
  CSP, proxy authentication boundary, CORS, network policies and independent
  security audit. The Vite dev server must never be exposed to a LAN/public
  address.
- No production-ready project-admin grant CRUD or self-service project
  list. Browser can address only one known, server-authorized resource.
- Do not merge the Draft PR or expose this UI until the full release matrix
  passes. No training dataset export in this scope.
