# Security Foundation — POC and Production Acceptance Matrix

**Status:** SPECIFICATION ONLY / **0 security gates executed** on this PR.
**Change scope:** docs only. No operator installed, no database migrated, no OpenBao initialized, no production traffic enabled.
**Baseline:** `master@b3e571ce07fd1fb5f49e6f0c08ddf64072c6b8d2`.
**Design:** [Security & Persistence Foundation](architecture/security-persistence-foundation.md).

## Release policy

**PASS means an independently reproducible result from the exact immutable candidate SHA and deployment digests.** Every gate records: candidate SHA, test environment/cluster and namespace, operator/chart/image digests, command/test source, expected/observed outcome, exit code, timestamp, non-secret log or redacted log hash, reviewer and proof URL. `SKIPPED`, `PENDING` and `BLOCKED` never count as PASS. Keep every negative test fail-closed. Run against dedicated POC databases; never reuse the Stage B `*_test` destructive contract DSN against CNPG production resources.

## Phased delivery

| Phase | Deliverable | No-go condition |
| --- | --- | --- |
| **S0: protect existing Go boundary (P0)** | OIDC or equivalent validated identity; Project RBAC; authenticated Agent identity; denial audit; production DSN/TLS fail-closed | Any anonymous cross-project write, register, lease, finalize, desired mutation or internal route access |
| **S1: CNPG as platform Store (P0)** | 3-member CNPG, strict TLS cert roles, migration Job, backup WAL, restore, old Go Store regression | DB/runtime DDL privilege, plaintext/password downgrade, skipped lease/tombstone gate, no proven PITR |
| **S2: OpenBao POC (P1 after S0)** | 3-member OpenBao service, Postgres storage vs Raft comparison, TLS HTTPS, role-bound Kubernetes auth, simple KV write/read | Root token in Kubernetes/Git/CI; `tls_disable=1`; unseal custody not defined; shared unlimited secret reader |
| **S3: workload integration (P1)** | Project-scoped `credentialBindingId`, CSI/Injector delivery, deny-by-default, audit without values | Any raw secret in desired/observed/evidence, Pod spec or logs; unauthorized workload can read other project |
| **S4: operational promotion** | cert rotation, primary failover, K8s restart, backup/PITR, DR drill and recovery handbook | Restoration cannot preserve generation/lease safety, auto-unseal circular dependency or unmeasured RTO/RPO |

## Negative-first test cases

| ID | Target | Test/action and expected result | Evidence | Status |
| --- | --- | --- | --- | --- |
| SEC-01 | Human authentication | anonymous PUT/DELETE ComputePool/Workload -> 401; invalid issuer/audience/expired OIDC token -> 401 | HTTP transcript, no credentials | BLOCKED — middleware missing |
| SEC-02 | Project authorization | project-A Developer mutates/deletes project-B resource -> 403/404, no state change | API + SQL before/after + audit | BLOCKED |
| SEC-03 | Agent identity | forged `clusterID`, invalid/expired Agent credential, replay after revoke -> rejected | wire test + server audit | BLOCKED |
| SEC-04 | Privileged routes | anonymous `/api/v1/internal/...`, `/agent/finalize-desired`, `/agent/reconcile-lease/claim` -> rejected | negative API tests | BLOCKED |
| SEC-05 | Production mode | missing DSN, in-memory fallback, insecure TLS, plaintext listener -> startup failure | process exit + config test | BLOCKED |
| CNPG-01 | HA/placement | 3 healthy PostgreSQL pods on distinct eligible worker nodes; no control-plane toleration | `kubectl cnpg status`, pod/node/zone mapping | PENDING |
| CNPG-02 | mTLS | `platform_runtime`/ `openbao_rw`: correct cert + `verify-full` succeeds; no cert, wrong CN, plaintext and wrong CA fail | positive/negative `psql` test matrix | PENDING |
| CNPG-03 | Least privilege | runtime role cannot `CREATE TABLE`, `ALTER ROLE`, access other DB, or read another schema; DML works only on authorized tables | SQL privilege-negative logs | PENDING |
| CNPG-04 | Stateful behavior | Go rebuild after CNPG primary failover; all named PostgreSQL lease/tombstone/generation contracts stay green | Go test transcripts + DB observation | PENDING |
| CNPG-05 | Synchronous tradeoff | fail primary with standby healthy; separately isolate all standbys: under `required` no successful writes without sync ack | timeline, commits/LSN, write failure/latency | PENDING |
| CNPG-06 | Backups | WAL archiving verified; Barman Plugin ScheduledBackup `method: plugin` completes; independent PITR restore verifies exact marker | ObjectStore/Backup status, marker, target LSN | PENDING |
| BAO-01 | Bootstrap | Helm HTTPS, chart `server.volumes` & `volumeMounts`, 3 pods eligible workers, no persistent local storage in Postgres variant | rendered Helm + running pods; negative HTTP test | PENDING |
| BAO-02 | DB table boundary | owner one-shot DDL succeeds; runtime `skip_create_table=true` works; runtime cannot DDL; no root/PG password in logs | SQL and OpenBao status, redacted logs | PENDING |
| BAO-03 | Isolation | Project-A SA reads only A path, Project-B only B; default SA/unknown namespace/read-all action denied | OpenBao/K8s auth and audit (no secret values) | PENDING |
| BAO-04 | Pod injection | authorized Job gets file from memory-backed volume; unauthorized Job cannot mount/read; no secret payload in Kubernetes PodSpec, desired, observed or evidence | Pod inspection + leakage scan | PENDING |
| BAO-05 | Credential rotation | CNPG cert auto-renewal detected; after OpenBao rolling reconnect new cert works; old cert is invalidated after expiry/rotation; no interrupted authorized reads beyond agreed SLO | Secret serial/fingerprint (not private key), connection checks | PENDING |
| BAO-06 | Recovery | rebuild OpenBao storage from PITR plus independently stored unseal/auto-unseal material; verify policy, encrypted KV and audit recovery without exporting values | restored status + redacted functional tests | PENDING |
| BAO-07 | Backend selection | measure PostgreSQL backend vs Integrated Raft: recovery, ops overhead, RTO, cost and lock/standby behavior; record decision | reproducible comparison, ADR update | PENDING |
| OPS-01 | Audit | all create/delete/secret access/denial traced to actor/project/action/generation; no raw token, cert, SQL password, secret material | structured events + redaction assertions | BLOCKED |
| OPS-02 | Upgrade/supply chain | pinned CNPG/OpenBao/Barman/cert-manager images and charts; digest + cosign policy; compatibility matrix; upgrade/rollback drill | verified digest identities, controlled upgrade logs | PENDING |
| OPS-03 | DR safety | restore old platform snapshot while provider objects still exist: no duplicate Job, no stale lease finalization, no generation rollback | restored Store + provider observation/replay evidence | PENDING |

## Golden Path regression invariant

**Keep the already-supported Kueue path unchanged**. The enhanced E2E must prove:

```text
authenticated Project Owner
 -> authorized ProjectBinding
 -> authorized ComputePool create
 -> Workload with opaque secret binding
 -> Kueue LocalQueue admission
 -> Pod with limited SA identity
 -> Secret injection, only within project scope
 -> provider Observation/Evidence (no secret bytes)
 -> authorized deletion
 -> provider cleanup
 -> tombstone
 -> CNPG failover / Agent takeover replay
 -> exactly one logical workload identity (not "exactly once" side effects)
```

Required pre-existing gates remain `make acceptance-contract`, `make e2e-golden`, `make supply-chain-check`; new security E2E gates are additional, never substitutions. `stage-b-local-gate.sh` is a **local test DB** gate and must not be redirected to a shared cluster or production.

## Open decisions that must be measured (do not assume)

1. **PostgreSQL or Raft for OpenBao?** For the first POC, test both. If PostgreSQL introduces unacceptable shared failure/recovery coupling, select Integrated Raft even though CNPG remains the platform DB.
2. **Database isolation:** Separate DBs + roles are the minimum; evaluate dedicated CNPG clusters across failure domains for critical production secrets.
3. **Unseal:** manual Shamir for non-production POC; independently rooted auto-unseal (KMS/HSM/Transit) for production if selected. Circular Transit against the same OpenBao is forbidden.
4. **Secret consumption:** prefer short-lived projected workload identity and memory-only Agent Injector/CSI. Kubernetes Secret sync needs an explicit exception, threat model and expiry review.
5. **Recovery targets:** propose **POC** targets only (`RPO=0` for acknowledged same-cluster synchronous commits while sync quorum exists; `RTO <= 30min` for a representative PITR exercise). Both require measurement and explicit scope; cross-region async DR RPO is not zero by default.
6. **Upgrade policy:** CNPG 1.30+ is required for `DatabaseRole.clientCertificate`; revalidate specific patch release, CRD schema and generated Secret paths before a manifest is considered deployable.

## First implementation batch (next PR; not part of this design PR)

- S0-1: authenticated Go Router / Agent API policy middleware + 401/403/404 negative tests.
- S0-2: production profile rejects memory fallback and insecure PostgreSQL DSN, while retaining explicit localhost dev compatibility.
- S1-1: isolated pinned CNPG POC manifests, runtime and migrator `DatabaseRole` objects, role-specific `pg_hba`, SQL negative test Job.
- S1-2: K8s Deployment for Go Control Plane with projected certs/CA and short DNS SAN-verified connection URL; do **not** migrate production data yet.
- S1-3: Barman Plugin backups, restore drill, then previously frozen PostgreSQL Store regression suite.
- S2-1: OpenBao Helm Postgres vs Raft POC and Kubernetes auth/deny-negative tests.
