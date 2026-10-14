# Security & Persistence Foundation: CNPG + OpenBao (ADR, proposed)

- **Status:** PROPOSED / design only — **not deployed, not accepted as production-ready**
- **Date:** 2026-10-14
- **Repository baseline:** `master` at `b3e571ce07fd1fb5f49e6f0c08ddf64072c6b8d2`
- **Related contracts:** [Go control plane](../CONTROL_PLANE_V2_GO.md), [recovery](../PROVIDER_RECOVERY_CONTRACT.md), [release gate](../RELEASE_ACCEPTANCE_V0_1.md)
- **Decision rule:** Adopt → Integrate → Contribute → Build. Do not build a custom KMS, secret operator, PostgreSQL operator or scheduler.

## 1. Problem and evidence from this repository

The canonical runtime is the **Go control plane + cluster agent**, not the legacy FastAPI/Celery stack. The Go service uses `DATABASE_URL` with `database/sql` + pgx; PostgreSQL persists desired state, observations, reconciliation leases and deletion tombstones. Existing PostgreSQL reconstruction and ownership/fencing tests are valuable and **must not be replaced** by HA promises.

Observed gaps on this baseline:
- `internal/platform/httpapi/router.go` registers public mutations and agent routes without an authentication/authorization middleware. **Not safe to expose as a multi-tenant production API.**
- `cmd/control-plane/main.go` accepts any database DSN, or silently falls back to in-memory state if it is missing. A production entrypoint must fail closed instead.
- `docker-compose.yml` and `.env.example` include *development-only* PostgreSQL passwords and cloud credential placeholders. They are not production delivery templates.
- No CNPG, OpenBao, production identity, backup/PITR or credential-rotation deployment has been implemented or E2E-tested on this branch.
- `scripts/verify-production-images.sh` currently reports an armed policy with **zero production images**. That is not evidence that actual CNPG/OpenBao images have been verified.

The security foundation is subordinate to the existing product contract:
`Project -> ComputePool -> Workload -> Kueue admission -> provider observation -> evidence -> finalization`.
Kueue remains the **only** accepted admission path; CNPG/OpenBao do not introduce a second scheduler, queue or desired-state authority.

## 2. Ownership and trust boundaries

```text
Human / Vue / CLI
    | OIDC user identity -> project-scoped policy (to be implemented)
    v
Go Control Plane  -------> PostgreSQL via TLS client cert
    |                         [management source of truth:
    |                          desired/observed/lease/tombstone/audit metadata]
    |
    +-- opaque CredentialBinding / secret reference
    |           (NO secret bytes in resource specs, responses or evidence)
    v
OpenBao HTTPS  <-------- Kubernetes SA/JWT auth (or approved workload identity)
    |            [policy per project, app and action]
    | PostgreSQL storage, TLS client cert, restricted DML
    v
CNPG cluster (OpenBao encrypted storage) ---> Barman Cloud backups + WAL
    ^
    | separate DB and roles from platform Store;
    | separate physical CNPG clusters for production isolation when justified
    |
Cluster Agent (identity separate from user and workload)
    | authenticated/fenced control-plane calls (to be implemented)
    v
Kubernetes / Kueue -> Pods -> mounted ephemeral secrets only when authorized
```

**Source-of-truth split:**
1. **Platform PostgreSQL** owns transactional product state and audit *metadata*. Preserve current CAS, generation, lease-owner and tombstone SQL semantics during migration.
2. **OpenBao** owns secret values, key versions, leases, policy and secret-access audit. PostgreSQL does not become a secret-management UI.
3. **Kubernetes** owns execution facts, Pod identity and projected tokens. It is *not* a second desired-state product database. Pod ServiceAccounts alone do not authorize platform Project mutations.
4. **CNPG** owns PostgreSQL availability/backup management, not platform tenancy or secret authorization.

**No recursive bootstrap dependency:** The CNPG operator, PostgreSQL, backup identity, DNS, cert-manager/CA and OpenBao initialization must be recoverable **without calling the same OpenBao instance**. Do not store CNPG's only backup/unlock credential exclusively in the OpenBao instance it is required to restore. Keep unseal/recovery key custody offline, split by designated maintainers; define an independent auto-unseal root if later selected.

## 3. Database topology and certificate contract

**POC:** one dedicated CNPG operator with two independent databases/roles; for availability and blast-radius assessment, measure the cost of separate CNPG clusters before production promotion. Never share DB owner/runtime identities:

| Purpose | DB role | Required privileges |
| --- | --- | --- |
| Platform schema migration | `platform_migrator` | schema DDL, designated maintenance window only |
| Platform Go runtime | `platform_runtime` | DML on necessary platform tables, no `CREATEDB`, `CREATEROLE`, superuser or schema CREATE |
| OpenBao schema bootstrap | `openbao_owner` | OpenBao schema/table ownership, one-shot Job only |
| OpenBao runtime storage | `openbao_rw` | SELECT/INSERT/UPDATE/DELETE on OpenBao storage + HA lock tables only |
| Backup | plugin/identity | independently scoped object store write/read as needed; not `openbao_rw` |

**CNPG version prerequisite:** `DatabaseRole.spec.clientCertificate` is available in CNPG **1.30+**. Gate manifests to the installed CRD and operator version; verify chart/operator/DB compatibility before upgrade. Do not rely on mutable PostgreSQL image tags in production.

For each runtime role: `DatabaseRole` generates `<databaserole-name>-client-cert` (`tls.crt`/`tls.key`); manually add role-specific, first-match `hostssl <db> <role> ... cert` and `hostnossl ... reject` rules to CNPG `postgresql.pg_hba`. Also deny/restrict all unintended authentication paths and apply NetworkPolicies: 'no password in URL' **alone does not enforce** certificate authentication. With a named PostgreSQL server and validated SAN, clients must use:
`sslmode=verify-full&sslrootcert=/.../ca.crt&sslcert=/.../tls.crt&sslkey=/.../tls.key`.
Use full Service DNS that matches the server certificate. Restrict mount access by runtime UID/GID and private key file mode; test actual `fsGroup` and `defaultMode` with deployed images rather than assuming all containers run under UID 26. The mounted private key remains a **Kubernetes Secret**, protected by Kubernetes RBAC, API encryption-at-rest and node boundaries. 'Passwordless' does **not** mean credential-free.

PostgreSQL privileges: explicitly `REVOKE CONNECT ON DATABASE ... FROM PUBLIC`, `REVOKE ALL ON SCHEMA public FROM PUBLIC`, grant only necessary CONNECT/USAGE/DML to runtime roles and owners. Use a migration/init Job separate from the live application. `DatabaseRole` does not itself grant per-table DML. Confirm grants using a negative privilege test, not only an application success test.

**Durability policy:** 3 instances, eligible distinct failure domains; `synchronous: {method: any, number: 1, dataDurability: required}` for security-critical stores. `required` sacrifices write availability when no synchronous standby can acknowledge commits. Do **not** claim cross-region or arbitrary-failure RPO=0; backup/PITR and asynchronous remote DR require independently measured RPO. CNPG failover is **not** a replacement for archived WAL and tested restore.

## 4. OpenBao: scope and integration choice

OpenBao with native `storage "postgresql"` and `ha_enabled=true` is a supported candidate. POC it against **Integrated Raft** as a simpler operational alternative; choose with measured steady-state cost, restore complexity and dependency depth. The official OpenBao v2.7 PostgreSQL backend describes HA and horizontal reads; do not assume read scalability or enable topology-specific features without tests.

For PostgreSQL mode:
- Define canonical, version-matched OpenBao KV/HA table DDL in a separate migration Job; `skip_create_table=true` ensures the runtime role cannot silently create tables.
- On the official Helm chart set `server.dataStorage.enabled=false` for PostgreSQL mode and use **`server.volumes` + `server.volumeMounts`** for the client certificate and CA. Enforce 3 OpenBao Pods on distinct eligible **worker** nodes; no production control-plane toleration.
- **Production listener HTTPS is mandatory.** The playground's `tls_disable=1` is dev-only. Use trusted TLS, `api_addr`/`cluster_addr`, secure probes, active/standby behavior and a tested init/unseal runbook.
- No root token, Shamir shares, `BAO_TOKEN`, client private key or secret value may be committed, pasted into CI logs, stored in control-plane DB, or embedded in a Pod command. Initialize/unseal with an interactive, out-of-band procedure. Define unseal/auto-unseal key custody and audit before production.
- CNPG client certificate renewal does **not** prove that long-lived OpenBao PostgreSQL connections have reloaded new TLS material. Schedule verified rollout/reconnect **after** Secret renewal but **before** old certificate expiry; measure failover during rotation. TLS listener auto-reload and PostgreSQL storage connection-pool reload are different mechanisms.

**Consumption default:** adopt OpenBao Kubernetes auth with narrow ServiceAccount/namespace/project policies; default to Agent Injector or CSI *without Kubernetes Secret sync* for Pods. Use workload identity for cloud APIs wherever available; use KV v2 for unavoidable long-lived keys, the DB secrets engine only for workloads that genuinely need ephemeral DB users, Transit/PKI only when there is a clear use case. Do not grant every workload cluster-wide read access to the same secret path.

**Future API contract (not implemented):** a Workload refers to an opaque `credentialBindingId` owned by its Project; the server resolves it after authorization, then creates a narrowly scoped projected volume / injection configuration. Never accept a caller-selected arbitrary OpenBao path as sufficient authorization. Secret value/lease/access metadata must not contaminate desired/observed JSON or evidence. Provider reconciliation must remain at-least-once and replay-safe without accidentally creating duplicate secret identities.

## 5. Identity, audit and production fail-closed order

P0 blockers **before any external exposure or production secrets**:
1. Authenticate human operators (OIDC; signed token verification, issuer/audience/expiry/JWKS rules), and authorize **Project** read/write/delete separately. Deny across-tenant access and unauthorized ComputePool/Workload create/delete. Existing Python auth does not protect the canonical Go Router.
2. Authenticate Cluster Agents with a distinct, cluster-bound machine identity (e.g. short-lived mTLS or independently validated SA JWT), enforce allowed `clusterID`, rotate/revoke; do not confuse a registered heartbeat with trusted identity.
3. Enforce production fail-closed config: missing database DSN, `sslmode=disable/require`, unverified TLS, in-memory Store fallback and insecure public HTTP are unacceptable in a production profile. Keep a distinct localhost development profile.
4. Persist append-only security audit with actor, action, project/resource, outcome, request/correlation ID, observed generation and *references* to secret events; redact tokens and values. Record denied access. Ensure the audit destination does not depend on an uninitialized secret service.
5. Introduce separate SecurityAdmin / PlatformAdmin / ProjectOwner / Developer / Viewer / ClusterAgent / Workload roles. Neither K8s cluster-admin nor OpenBao root token becomes a normal platform administrator session.

## 6. Availability, backup and change controls

- Use the official **Barman Cloud Plugin** with `ObjectStore`, WAL archiving, and `ScheduledBackup.spec.method=plugin`; version/pin/operator compatibility checks precede applying manifests. Store backups in a distinct failure domain, with encryption, retention, immutable backup protections where supported and independent recovery identity.
- Test **PITR restore of BOTH** platform database and OpenBao storage, including OpenBao unseal recovery materials. Restore must recheck Project RBAC, CAS/lease fencing, tombstones and Secret auth; DB-only green is not product recovery.
- A restored platform must **not blindly replay stale desired intent** into live Kubernetes: maintenance/read-only mode -> identify restored commit point and resource generations -> reconcile/fence against current K8s facts -> authorized resume.
- Run certificate expiry/CA rotation, backup loss, primary failover, loss of two standby nodes, long-running OpenBao sessions, agent takeover, denied secret access and K8s API outages as independent failure experiments.
- Production rollout is a sequence: baseline freeze -> isolated CNPG proof -> credential/RBAC boundary -> OpenBao proof -> backup/PITR & chaos -> controlled promotion. No auto-merge and no `READY` merely because Helm rendered.

See [Security Foundation Acceptance Matrix](../SECURITY_FOUNDATION_ACCEPTANCE.md) for executable evidence criteria and negative gates.

## 7. References (official)

- CloudNativePG replication: https://cloudnative-pg.io/docs/current/replication/
- CloudNativePG DatabaseRole/client certificate: https://cloudnative-pg.io/docs/devel/declarative_role_management/
- CloudNativePG backups: https://cloudnative-pg.io/docs/current/backup/
- Barman Cloud Plugin: https://cloudnative-pg.io/plugin-barman-cloud/docs/usage/
- OpenBao PostgreSQL storage: https://openbao.org/docs/configuration/storage/postgresql/
- OpenBao Kubernetes integrations: https://openbao.org/docs/platform/k8s/
- OpenBao Helm values: https://github.com/openbao/openbao-helm/blob/main/charts/openbao/values.yaml
