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

Mandatory configuration:

```bash
export MANAGEMENT_DATABASE_URL='postgres://...'
export OIDC_ISSUER='https://idp.example.com/realms/compute'
export OIDC_INTROSPECTION_URL='https://idp.example.com/realms/compute/protocol/openid-connect/token/introspect'
export OIDC_CLIENT_ID='management-api'
export OIDC_CLIENT_SECRET='...'
export OIDC_AUDIENCE='management-api'
export MANAGEMENT_LISTEN_ADDR=127.0.0.1:8081
go run ./cmd/management-api
```

Secrets never belong in the frontend or in committed configuration.
Production must also restrict the Postgres management DB role, TLS upstream,
and all network policies.

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
