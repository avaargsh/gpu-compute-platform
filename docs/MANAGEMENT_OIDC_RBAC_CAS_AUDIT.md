# Management API v0 — authenticated, project-scoped mutation candidate

Status: **draft, under validation**. This is the start of Issue #63, not a
declaration that the full production portal is ready.

## Explicit security boundary

`cmd/management-api` is a **separate service** from the pre-existing Go
Control Plane / Cluster Agent API. Only `/healthz` and scoped
`/api/v1/management/...` GET/PUT/DELETE plus owner-scoped operation
receipt GET are registered. The legacy
`/api/v1/agent`, `/api/v1/internal` and legacy resource routes are never
registered or proxied in this service. The management listener is
**127.0.0.1:8081 only**, until independently reviewed TLS ingress / reverse
proxy / Agent mTLS / network policy are deployed. Keep the old :8080 Agent
listener private.

Identity provider: OAuth 2.0 RFC 7662 **access-token introspection** at
an HTTPS introspection endpoint controlled by an OIDC issuer. It checks
`active`, exact configured issuer and audience, nonempty stable `sub`,
expiry and Bearer token type. Client credentials are given only to the
management server, never Vue. Every request re-introspects so IdP revocation
can take effect. This intentionally does not accept ID tokens, unsigned
JWTs, forwarded identity headers, or browser-selected roles.

This is not a complete interactive Authorization Code + PKCE OIDC login:
that UI work, IdP integration verification and public TLS deployment are
separate release gates. Provider introspection must return `iss`,
`sub`, `aud`, `exp`, `active`; providers that omit any of these claims
will fail closed. Do not weaken validation to accommodate incomplete IdP
claims without a separately reviewed protocol adapter.

## PostgreSQL setup

Use a separately provisioned database principal for this service. Trusted
DBA automation applies existing `internal/store/postgres/schema.sql` and
new `internal/platform/management/schema.sql` to the intended database.
The service deliberately does **not** auto-migrate arbitrary databases.

Grant project access **only** through DBA/controlled administration, e.g.:

```sql
INSERT INTO management_project_grants
    (issuer, subject, project_id, role)
VALUES ('https://idp.example.com/realms/compute', 'idp-subject-uuid',
        'research', 'operator');
```

Roles: `viewer` can GET; `operator` and `admin` can PUT and DELETE.
Grant queries are server-side and locked during mutations; the browser and
token can never self-assert membership or change role. Use append-only DB
permissions for `management_operation_audit` (SELECT/INSERT, no
UPDATE/DELETE/TRUNCATE), separate DBA role for grants and schema.

### Reproducible PostgreSQL least-privilege admission

After applying both schemas as DBA, provision a dedicated NOLOGIN group
role and a **separate credential-managed LOGIN** principal; do not embed
passwords or a superuser DSN in the repository. A trusted DBA may apply the
checked-in grant template:

```sql
CREATE ROLE gpu_management_runtime NOLOGIN;
-- Apply internal/platform/management/runtime_role.sql as a trusted DBA.
-- Grant that role only to the separately provisioned Management API login.
```

`internal/platform/management/runtime_role.sql` grants only the following
runtime permissions:

| Object | Application role privileges |
| --- | --- |
| `management_project_grants`, `project_bindings`, `cluster_bindings`, `deletion_tombstones` | SELECT |
| `desired_resources` | SELECT, INSERT, UPDATE (Generation CAS and deletion finalizer) |
| `management_operation_audit` | SELECT, INSERT (append-only; no UPDATE/DELETE/TRUNCATE) |
| Cluster Agent records, observations, placement migrations, leases and other tables | No privileges |

Do not grant the Management API login table ownership, superuser,
`BYPASSRLS`, role membership inherited from the schema owner, or any
additional `PUBLIC`/group grants that expand these capabilities. DBAs must
verify **effective** grants of the actual LOGIN role; direct table grants
are not a sufficient check if that account inherits another broad role.
PostgreSQL role isolation constrains the Management service's SQL authority;
issuer/project RBAC remains enforced in the application and this template
is **not** a row-level security policy.

The integration test `TestManagementRuntimeRoleLeastPrivilegeWithPostgres`
creates a disposable NOLOGIN role in `TEST_POSTGRES_DSN`, executes the
exact grant template and tests permitted Desired/audit SQL alongside denied
audit updates/deletes, grant/binding edits, other service tables and schema
creation. The test transaction rolls back its data probes and drops the
temporary role. Never point `TEST_POSTGRES_DSN` at production.

Mandatory configuration:

```bash
export MANAGEMENT_DATABASE_URL='postgres://...'
export OIDC_ISSUER='https://idp.example.com/realms/compute'
export OIDC_INTROSPECTION_URL='https://idp.example.com/realms/compute/protocol/openid-connect/token/introspect'
export OIDC_CLIENT_ID='management-api'
export OIDC_CLIENT_SECRET='...'
export OIDC_AUDIENCE='management-api'
export MANAGEMENT_LISTEN_ADDR=127.0.0.1:8081
export MANAGEMENT_BROWSER_ORIGIN='http://127.0.0.1:3002' # explicit loopback UI opt-in
go run ./cmd/management-api
```

Secrets never belong in the frontend or in committed configuration.
Production must also restrict the Postgres management DB role, TLS upstream,
and all network policies.

### Browser Origin gate

The management API denies **all Origin-bearing write requests by default**.
This protects the CLI-only default even if a browser can reach loopback Go.
For the local PKCE workbench alone, set
`MANAGEMENT_BROWSER_ORIGIN=http://127.0.0.1:3002` in the Go process. It
must be exactly a loopback HTTP origin, never `*`, `null`, `localhost`, a
LAN address or a public ingress. Requests with a different/duplicate Origin
or `Sec-Fetch-Site: cross-site` fail with 403 before any Desired/audit write.
CLI requests without Origin remain supported. Browser Origin admission is
not identity or tenant authorization; every request still uses OIDC provider
introspection, DB project grants, CAS and append-only audit.

This setting does not grant production CORS. External TLS ingress,
proxy-level identity and machine Agent authorization remain blocked pending
independent review.

## REST operation contract

```
/api/v1/management/clusters/{clusterID}/projects/{projectID}/resources/{kind}/{resourceID}
```

`kind` is only `ComputePool` or `Workload`.

- `GET`: requires Viewer+, returns one scoped Desired resource and
  strong Generation ETag. Not readiness, workload admission, or independent
  observation.
- **All PUT and DELETE requests require `Idempotency-Key`**, exactly 32
  lowercase hex digits generated from 16 bytes of cryptographically secure
  randomness by the client (missing key: HTTP 428; malformed: HTTP 400).
  The first successful operation atomically stores the ID, exact request
  fingerprint and verified caller in its audit row. Retrying the **same**
  operation with the same key returns the original ETag/result without
  another resource mutation or audit event. Reusing the key with another
  payload, caller, scope, resource, Generation or method returns 409.
  A request whose outcome is uncertain because of a network failure should
  be retried **only with the original key and identical body**, not a new
  one. This does not assert Kubernetes reconciliation/execution success.
- `PUT`: requires Operator+, JSON `expectedGeneration` integer. To
  create, supply 0; to update an existing **ComputePool**, supply its exact
  current Generation. The new Generation increments by one under a PostgreSQL
  advisory lock and a `SERIALIZABLE` transaction. Active Workloads are
  immutable: use the delete/finalizer path and a separately reviewed
  new-identity workflow rather than silent in-place replacement. Binding
  checks force Project and Pool to the same cluster and use the existing
  Kueue Provider identity. New tombstone identities cannot silently
  restart at Generation 1.
- `DELETE`: requires Operator+ and exact `If-Match: "N"`. Marks Desired
  deleting, adds provider cleanup finalizer, and returns 202. It does
  **not** delete a Kubernetes Pod or remove a row directly. Active workload
  children prevent ComputePool deletion.
- Successful PUT / DELETE inserts an append-only
  `management_operation_audit` record with new random request ID, verified
  issuer + subject, project/cluster/resource identity, action, requested
  and resulting generations, and before/after **spec SHA-256 hashes** — not
  secrets or entire workload specs. The audit insert and desired mutation
  commit in **one database transaction**. Audit failures roll back Desired
  changes. 403/409 do not produce a successful-write audit event.

```bash
OPERATION_ID="$(openssl rand -hex 16)"    # retain for retries
curl -sS -X PUT \
  -H "Authorization: Bearer $OIDC_ACCESS_TOKEN" \
  -H "Idempotency-Key: $OPERATION_ID" \
  -H "Content-Type: application/json" \
  --data-binary @compute-pool-request.json \
  http://127.0.0.1:8081/api/v1/management/clusters/cluster-a/projects/research/resources/ComputePool/pool-a
```

```json
{
  "expectedGeneration": 0,
  "spec": {
    "accelerators": [{"class": "h100", "quota": 2}],
    "acceleratorBindings": [{
      "class": "h100", "resourceName": "nvidia.com/gpu", "flavor": "h100"
    }],
    "scheduling": {"mode": "kueue"}
  }
}
```

Workload creation additionally requires `"poolId":"existing-pool"` and a
Workload `spec` with `image`, `command`, and
`accelerator: {class, quota}`.

## Operation commit receipt and safe recovery

A successful PUT or DELETE stores an immutable Desired-state audit receipt in
the *same PostgreSQL transaction*. After an HTTP timeout, disconnect or
ambiguous `503` response, do **not** assume the mutation failed and do not
mint a new operation ID. Re-introspect the current access token, then query:

```text
GET /api/v1/management/clusters/{clusterID}/projects/{projectID}/operations/{operationID}
Authorization: Bearer <access-token>
```

The receipt endpoint requires the **same verified issuer and subject**
responsible for the write **and a live operator/admin project grant**.
Unauthorized or nonexistent operation IDs both return 404, while missing
or invalid access tokens return 401. Project membership downgrade/revocation
removes access even to earlier receipts.

A committed request returns HTTP 200 with `Cache-Control: no-store`,
`X-Request-ID`, the resulting Generation `ETag`, and a JSON object:

```json
{
  "requestId": "0123456789abcdef0123456789abcdef",
  "status": "desired-committed",
  "clusterId": "cluster-a",
  "projectId": "research",
  "kind": "ComputePool",
  "resourceId": "pool-a",
  "action": "create",
  "expectedGeneration": 0,
  "resultingGeneration": 1,
  "requestDigest": "sha256-hex",
  "committedAt": "2026-10-11T00:00:00Z"
}
```

`requestDigest` is an actual 64-character lowercase SHA-256 hex digest,
not the placeholder shown here. `status=desired-committed` **proves a
management Desired write and its audit commit only**; it is NOT proof of
Kueue admission, provider resource existence, Agent ownership, or Workload
execution. For a 404 where the caller still has a grant, the operation is
not known to have committed at query time; repeat the **identical** original
PUT/DELETE with the **same** Idempotency-Key, then look up the receipt
again. A late in-flight transaction may still win; the operation-level
idempotency key prevents a second write.

The receipt only reports the initiating subject's own operation. Audit
export to project admins and independent execution attestation are separate
future endpoints, not implicit in this API.

## Local operator readiness and identity provider failures

The management CLI defaults to `http://127.0.0.1:8081`. To use a
concurrent local test instance, pass `-api http://127.0.0.1:18081` to
every `managementctl` command; only exact numeric `127.0.0.1` and an
explicit valid port are accepted. It rejects remote hosts, `localhost`
aliases, URL user information, query strings, paths and HTTPS redirection.
The management API itself must still be started on loopback.

Identity provider failures are distinct from invalid credentials: an
inactive/expired/wrong-audience access token returns **401**; a failed
introspection transport, provider HTTP non-200, malformed provider response
or invalid server-side provider configuration returns **503** without
performing any database operation. The CLI shows the status and retains
the original operation ID for any ambiguous mutating request. No access
tokens or IdP responses are printed in browser/client error bodies.

CI runs a real `managementctl` → loopback HTTP Management handler →
PostgreSQL acceptance, using a disposable **test-only IdP double**.
Its success is not a substitute for a production OIDC provider
interoperability and login review.

## Loopback management client (no browser login bypass)

The CLI is a minimal operator interface for **existing, externally obtained
OIDC access tokens**. It never handles the IdP client secret, does not
generate/trust a browser role and is hard-pinned to local
`http://127.0.0.1:8081`; redirect-following and HTTP proxies are disabled.
Keep port 8081 inaccessible from other hosts.

```bash
export MANAGEMENT_ACCESS_TOKEN='<provider-issued-access-token>'
# A unique operation ID is generated before each distinct mutation.
OP_ID="$(go run ./cmd/managementctl -action new-key)"

# JSON request file must include expectedGeneration and typed resource spec.
go run ./cmd/managementctl \
  -action put -cluster cluster-a -project research \
  -kind ComputePool -resource pool-a \
  -file compute-pool-request.json -operation-id "$OP_ID"

# After a broken connection, query the original operation ID. No newly
# generated ID may be used for a retry of that ambiguous operation.
go run ./cmd/managementctl \
  -action receipt -cluster cluster-a -project research \
  -operation-id "$OP_ID"

# If receipt is unavailable, retry the exact same file, operation ID and
# resource path. To delete a different resource, generate a NEW operation ID.
DEL_ID="$(go run ./cmd/managementctl -action new-key)"
go run ./cmd/managementctl \
  -action delete -cluster cluster-a -project research \
  -kind Workload -resource job-a -generation 1 \
  -operation-id "$DEL_ID"
```

Credentials are read from `MANAGEMENT_ACCESS_TOKEN` (not command arguments)
and the CLI prints the operation ID **before** sending a mutating request.
Record that ID in an external trusted operator journal if automation needs
crash recovery. This CLI is not a replacement for reviewed browser OIDC
Authorization Code + PKCE login, CSRF protection and project-scoped Portal
workflows.

## Release blockers

- IdP interoperability: issuer discovery, documented provider setup,
  revocation/regrant, token rotation, MFA and login (Authorization Code +
  PKCE).
- PostgreSQL negative matrix: concurrent creation + update CAS, grant
  revocation, audit failure rollback, deletion Finalizer and no cross-project
  mutation. Concurrent attempts must yield exactly one committed write and
  one append-only audit receipt per Generation. Replay after ambiguous
  transport commit must be independently chaos-verified against PostgreSQL
  (the token-introspection tests use a local IdP double).
- Operation ownership and ambiguous database commit outcomes; no
  exactly-once / retry guarantees are claimed from HTTP 204 alone.
- Project resource policy/quota/admission: model `quota` is user intent,
  not Kueue admission or actual device allocation.
- Complete Vue authenticated action flow; browser E2E and malicious
  request tests; audited read access review.
- Dedicated TLS reverse proxy and Agent mTLS (or authenticated machine
  identity) before any network exposure.
- Retain existing golden path (Kueue only) and all Stage B contracts.
- **No training-dataset publishing in this milestone.**
