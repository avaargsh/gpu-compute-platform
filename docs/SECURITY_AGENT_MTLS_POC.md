# Security S0 — Cluster Agent mTLS identity PoC

**Status: implementation candidate / NOT production-ready.**
This branch is stacked on [bootstrap fail-closed PR #78](https://github.com/avaargsh/gpu-compute-platform/pull/78).
It implements an intentionally **loopback-only Agent protocol**; it does not implement human OIDC, Project RBAC, identity enrollment/revocation, audit persistence or production ingress.

## Boundary and runtime behavior

- Go HTTPS listener verifies Agent client certificates using a **dedicated client CA**, TLS 1.3 minimum, and Go's x509 chain validation. HTTP forwarded-cert headers are ignored.
- Agent routes require a single verified leaf certificate with ClientAuth EKU and an exact SPIFFE URI SAN:
  `spiffe://<trust-domain>/cluster/<cluster-id>/agent`
- The URI path is strictly scoped: its cluster ID must match each Agent request's `clusterId` (GET query or POST JSON). Unknown identities, forged headers, cross-cluster requests, duplicates and oversized bodies are rejected **before** reaching the Store.
- In this proof the secured listener exposes only `/api/v1/agent/*` and `GET /healthz`. Management/admin/internal endpoints are 404. There is no privileged catch-all route.
- The old local HTTP/anonymous API remains usable only in an explicit development setup and is **not safe to expose**. Production profile continues to refuse startup.

## Server configuration (separate, isolated CNPG PoC)

The server must already have a real CNPG PostgreSQL Store, roles and projected certificates; these instructions do not provision databases or PKI:

```bash
export CONTROL_PLANE_PROFILE=poc
export CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:8080
export DATABASE_URL='postgres://platform_runtime@platform-db-rw.security.svc:5432/platform?sslmode=verify-full&sslrootcert=/etc/pg/ca.crt&sslcert=/etc/pg/tls.crt&sslkey=/etc/pg/tls.key'
export CONTROL_PLANE_TLS_CERT=/etc/agent-tls/server.crt
export CONTROL_PLANE_TLS_KEY=/etc/agent-tls/server.key
export CONTROL_PLANE_AGENT_CLIENT_CA=/etc/agent-tls/agent-issuing-ca.crt
export CONTROL_PLANE_AGENT_TRUST_DOMAIN=platform.example
./control-plane
```

The HTTPS server certificate needs a DNS/IP SAN matching what the Agent connects to; its private key is server-only. The Agent issuing CA must be separately managed from the PostgreSQL client CA. Do not put real keys into GitHub. Empty or incomplete TLS settings fail closed. This PoC still binds loopback only; port-forwarding it outside a controlled test environment defeats the trust boundary.

## Client configuration

Issue a separate short-lived client certificate per cluster with ClientAuth EKU and URI SAN
`spiffe://platform.example/cluster/kind-golden/agent` from the trusted Agent CA. The Agent must have the HTTPS server CA as well.

```bash
export CLUSTER_ID=kind-golden
export CONTROL_PLANE_URL=https://localhost:8080
export CLUSTER_AGENT_TLS_CA=/etc/agent-tls/control-plane-server-ca.crt
export CLUSTER_AGENT_TLS_CERT=/etc/agent-tls/agent-client.crt
export CLUSTER_AGENT_TLS_KEY=/etc/agent-tls/agent-client.key
./cluster-agent
```

The client verifies the server hostname and CA with standard Go TLS. No `InsecureSkipVerify` option exists. In development only, `CONTROL_PLANE_URL=http://127.0.0.1:8080` remains supported; unauthenticated remote HTTP is rejected.

## Negative-first evidence required

| Test | Required result |
| --- | --- |
| No client certificate / forged X-Forwarded-Client-Cert | 401 |
| Correctly CA-signed cert but wrong Agent URI/trust domain | 401 |
| Agent for cluster A requests cluster B desired, report, lease or finalize | 403 and no DB state change |
| Invalid/duplicated cluster ID or oversized JSON | 400 / 413 |
| Agent cert attempts management, cluster status, project binding or internal desired APIs | 404 |
| Correct Agent identity with matching cluster | register/pull/lease/report works |
| HTTPS server CA/hostname mismatch on client | TLS handshake fails |
| Agent cert expired or issued by untrusted CA | TLS handshake fails |
| Existing Stage B kind + Kueue HTTP loopback Golden Path | unchanged green |

Tests using a fabricated request TLS state prove the middleware policy only. A live TLS handshake test with real CA/certificates is required before promoting this PoC. Similarly, the existing Go unit/e2e suites remain mandatory.

## Explicit non-goals and blockers

- **No certificate issuance engine:** Adopt cert-manager or a recognized workload identity/PKI provider; do not invent one.
- **No production Agent revocation yet:** design issuance, rotation, short TTL, namespace/cluster binding, revocation and trust-domain migration before production.
- **No human authentication:** OIDC with verified JWT/JWKS issuer/audience plus server-side Project RBAC remains tracked in #63 and #79.
- **No claim of DB mTLS completion:** CNPG role, pg_hba, backups, restore and production image attestation are separate in #77.
- **No widened network exposure or merged production config.**

Until all AUTH-01..AUTH-10 and recovery gates pass, **production stays blocked**.
